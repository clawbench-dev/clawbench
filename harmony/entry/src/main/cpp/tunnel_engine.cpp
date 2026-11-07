#include "tunnel_engine.h"
#include "trusted_ca.h"
#include <libssh2.h>
#include <nghttp2/nghttp2.h>
#include <mbedtls/ctr_drbg.h>
#include <mbedtls/entropy.h>
#include <mbedtls/ssl.h>
#include <mbedtls/x509_crt.h>
#include <nlohmann/json.hpp>
#include <algorithm>
#include <atomic>
#include <cerrno>
#include <chrono>
#include <condition_variable>
#include <cstring>
#include <deque>
#include <fcntl.h>
#include <functional>
#include <memory>
#include <mutex>
#include <netdb.h>
#include <netinet/in.h>
#include <poll.h>
#include <regex>
#include <stdexcept>
#include <string>
#include <sys/socket.h>
#include <thread>
#include <unistd.h>
#include <unordered_map>
#include <vector>

namespace clawbench {
namespace {
using Json = nlohmann::json;
using Clock = std::chrono::steady_clock;
constexpr size_t MAX_BUFFER = 256 * 1024;
constexpr size_t MAX_CONNECTIONS = 32;
constexpr size_t MAX_MAPPINGS = 32;
struct Failure : std::runtime_error {
    std::string type;
    std::string fingerprint;
    Failure(const std::string& type, const std::string& message, const std::string& fingerprint = "")
        : std::runtime_error(message), type(type), fingerprint(fingerprint) {}
};
struct Descriptor {
    int fd = -1;
    explicit Descriptor(int fd = -1) : fd(fd) {}
    ~Descriptor() { if (fd >= 0) close(fd); }
    Descriptor(const Descriptor&) = delete;
    Descriptor& operator=(const Descriptor&) = delete;
    int release() { const int result = fd; fd = -1; return result; }
};
struct Config {
    std::string kind, origin, host, authority, cookie, password, username, hostKey;
    int port = 0;
    int sshPort = 0;
    bool tls = false;
};
struct Mapping {
    int port = 0, target = 0;
    std::string host, direction;
    std::atomic<bool> live{true};
    std::mutex mutex;
    std::condition_variable ready;
    bool settled = false, ok = false;
    std::string reason;
    void complete(bool success, const std::string& why = "") {
        std::lock_guard<std::mutex> lock(mutex);
        if (settled) return;
        settled = true;
        ok = success;
        reason = why;
        ready.notify_all();
    }
};
struct State {
    Config config;
    std::atomic<bool> live{true}, connected{false};
    std::atomic<unsigned> connections{0};
    std::mutex mutex;
    std::unordered_map<std::string, std::shared_ptr<Mapping>> mappings;
    std::string error, errorType;
    void fail(const std::string& type, const std::string& message) {
        std::lock_guard<std::mutex> lock(mutex);
        errorType = type;
        error = message;
        connected = false;
        live = false;
    }
};
struct ConnectionSlot {
    std::shared_ptr<State> state;
    explicit ConnectionSlot(const std::shared_ptr<State>& state) : state(state) {
        if (++state->connections > MAX_CONNECTIONS) {
            --state->connections;
            throw Failure("capacity", "Too many tunnel connections");
        }
    }
    ~ConnectionSlot() { --state->connections; }
};
std::mutex currentMutex, commandMutex;
std::shared_ptr<State> current;
std::once_flag initFlag;
std::atomic<uint64_t> cancelEpoch{0};

void Stop(const std::shared_ptr<State>& state)
{
    if (!state) return;
    state->live = false;
    state->connected = false;
    std::lock_guard<std::mutex> lock(state->mutex);
    for (auto& mapping : state->mappings) {
        mapping.second->live = false;
        mapping.second->ready.notify_all();
    }
}

bool Alive(const std::shared_ptr<State>& state, const std::shared_ptr<Mapping>& mapping = nullptr)
{
    return state->live && (!mapping || mapping->live);
}
void CheckAlive(const std::shared_ptr<State>& state, const std::shared_ptr<Mapping>& mapping = nullptr)
{
    if (!Alive(state, mapping)) throw Failure("network", "Tunnel cancelled");
}
int Port(const Json& input, const char* name)
{
    if (!input.contains(name) || !input[name].is_number_integer()) throw Failure("invalid", "Invalid port");
    const int port = input[name].get<int>();
    if (port < 1 || port > 65535) throw Failure("invalid", "Invalid port");
    return port;
}
std::string Host(const std::string& input)
{
    const std::string host = input.empty() ? "127.0.0.1" : input;
    if (host.size() > 253 || !std::regex_match(host, std::regex("[a-zA-Z0-9.:-]+"))) {
        throw Failure("invalid", "Invalid target host");
    }
    return host;
}
Config ParseConfig(const Json& input)
{
    Config config;
    config.kind = input.value("transport", "");
    config.origin = input.value("origin", "");
    std::smatch match;
    if ((config.kind != "ssh" && config.kind != "h2") || config.origin.size() > 2048 ||
        !std::regex_match(config.origin, match,
            std::regex("^(https?)://(\\[[0-9a-fA-F:]+\\]|[a-zA-Z0-9.-]+)(?::([0-9]{1,5}))?$"))) {
        throw Failure("invalid", "Invalid tunnel server");
    }
    config.tls = match[1] == "https";
    config.authority = match[2].str() + (match[3].matched ? ":" + match[3].str() : "");
    config.host = match[2].str();
    if (config.host.front() == '[') config.host = config.host.substr(1, config.host.size() - 2);
    config.port = match[3].matched ? std::stoi(match[3]) : config.tls ? 443 : 80;
    if (config.port < 1 || config.port > 65535) throw Failure("invalid", "Invalid server port");
    config.cookie = input.value("cookie", "");
    config.password = input.value("password", "");
    config.username = input.value("username", "clawbench");
    config.hostKey = input.value("hostKey", "");
    config.sshPort = input.value("sshPort", 0);
    if (config.cookie.size() > 32768 || config.cookie.find_first_of("\r\n") != std::string::npos ||
        config.password.size() > 4096 || config.username.size() > 128 ||
        (!config.hostKey.empty() && !std::regex_match(config.hostKey, std::regex("[0-9a-f]{64}"))) ||
        (config.kind == "ssh" && (config.sshPort < 1 || config.sshPort > 65535))) {
        throw Failure("invalid", "Invalid tunnel configuration");
    }
    return config;
}
void NonBlocking(int fd)
{
    if (fcntl(fd, F_SETFL, fcntl(fd, F_GETFL, 0) | O_NONBLOCK) < 0) throw Failure("network", "Socket setup failed");
}
int Dial(const std::string& host, int port, const std::shared_ptr<State>& state,
    const std::shared_ptr<Mapping>& mapping = nullptr)
{
    CheckAlive(state, mapping);
    addrinfo hints{};
    hints.ai_socktype = SOCK_STREAM;
    hints.ai_family = AF_UNSPEC;
    addrinfo* addresses = nullptr;
    if (getaddrinfo(host.c_str(), std::to_string(port).c_str(), &hints, &addresses) != 0) {
        throw Failure("network", "Tunnel host could not be resolved");
    }
    std::unique_ptr<addrinfo, decltype(&freeaddrinfo)> owner(addresses, freeaddrinfo);
    CheckAlive(state, mapping);
    const auto deadline = Clock::now() + std::chrono::seconds(10);
    for (auto* address = addresses; address; address = address->ai_next) {
        CheckAlive(state, mapping);
        Descriptor socketFd(socket(address->ai_family, SOCK_STREAM, 0));
        if (socketFd.fd < 0) continue;
        NonBlocking(socketFd.fd);
        const int code = connect(socketFd.fd, address->ai_addr, address->ai_addrlen);
        if (code == 0) {
            CheckAlive(state, mapping);
            return socketFd.release();
        }
        if (errno != EINPROGRESS) continue;
        while (Clock::now() < deadline && Alive(state, mapping)) {
            pollfd wait{socketFd.fd, POLLOUT, 0};
            if (poll(&wait, 1, 100) <= 0) continue;
            int error = 0;
            socklen_t length = sizeof(error);
            if (getsockopt(socketFd.fd, SOL_SOCKET, SO_ERROR, &error, &length) == 0 && error == 0) {
                CheckAlive(state, mapping);
                return socketFd.release();
            }
            break;
        }
    }
    throw Failure("network", "Tunnel target could not be reached");
}
struct PendingDial {
    std::atomic<bool> done{false};
    Descriptor socketFd;
    std::unique_ptr<ConnectionSlot> slot;
    explicit PendingDial(const std::shared_ptr<State>& state) : slot(std::make_unique<ConnectionSlot>(state)) {}
};
std::shared_ptr<PendingDial> StartDial(const std::shared_ptr<State>& state, const std::shared_ptr<Mapping>& mapping)
{
    auto pending = std::make_shared<PendingDial>(state);
    std::thread([pending, state, mapping] {
        try {
            Descriptor connected(Dial(mapping->host, mapping->target, state, mapping));
            CheckAlive(state, mapping);
            pending->socketFd.fd = connected.release();
        } catch (...) {}
        // Only POSIX I/O runs here. The owner consumes fd/slot after this release.
        pending->done.store(true, std::memory_order_release);
    }).detach();
    return pending;
}
void Erase(std::vector<uint8_t>& data, size_t size)
{
    data.erase(data.begin(), data.begin() + static_cast<std::ptrdiff_t>(size));
}
struct Ssh {
    std::shared_ptr<State> state;
    std::shared_ptr<Mapping> mapping;
    Descriptor socketFd;
    LIBSSH2_SESSION* session = nullptr;
    Ssh(const std::shared_ptr<State>& state, const std::shared_ptr<Mapping>& mapping = nullptr)
        : state(state), mapping(mapping), socketFd(Dial(state->config.host, state->config.sshPort, state, mapping)) {
        session = libssh2_session_init();
        if (!session) throw Failure("network", "SSH allocation failed");
        libssh2_session_set_blocking(session, 0);
        libssh2_session_flag(session, LIBSSH2_FLAG_SIGPIPE, 0);
    }
    ~Ssh() { finish(); }
    void finish(LIBSSH2_CHANNEL* channel = nullptr, LIBSSH2_LISTENER* listener = nullptr) {
        if (session) {
            shutdown(socketFd.fd, SHUT_RDWR);
            // Let libssh2 observe EOF before freeing channels with pending packets.
            if (channel || listener) {
                LIBSSH2_POLLFD descriptor{};
                descriptor.type = channel ? LIBSSH2_POLLFD_CHANNEL : LIBSSH2_POLLFD_LISTENER;
                if (channel) descriptor.fd.channel = channel;
                else descriptor.fd.listener = listener;
                descriptor.events = LIBSSH2_POLLFD_POLLIN;
                libssh2_poll(&descriptor, 1, 0);
            }
            libssh2_session_set_blocking(session, 1);
            libssh2_session_set_timeout(session, 500);
            libssh2_session_free(session);
            session = nullptr;
        }
    }
    template<class Operation> int retry(Operation operation) {
        const auto deadline = Clock::now() + std::chrono::seconds(15);
        int result;
        while ((result = operation()) == LIBSSH2_ERROR_EAGAIN) {
            CheckAlive(state, mapping);
            if (Clock::now() >= deadline) throw Failure("network", "SSH operation timed out");
            wait(100);
        }
        return result;
    }
    void wait(int milliseconds) {
        const int directions = libssh2_session_block_directions(session);
        pollfd item{socketFd.fd, static_cast<short>(
            ((directions & LIBSSH2_SESSION_BLOCK_INBOUND) ? POLLIN : 0) |
            ((directions & LIBSSH2_SESSION_BLOCK_OUTBOUND) ? POLLOUT : 0)), 0};
        if (!item.events) item.events = POLLIN;
        poll(&item, 1, milliseconds);
    }
    void authenticate() {
        if (retry([&] { return libssh2_session_handshake(session, socketFd.fd); }) != 0) {
            throw Failure("network", "SSH handshake failed");
        }
        const auto* hash = reinterpret_cast<const unsigned char*>(libssh2_hostkey_hash(session, LIBSSH2_HOSTKEY_HASH_SHA256));
        if (!hash) throw Failure("hostkey", "SSH host key fingerprint unavailable");
        std::string fingerprint;
        static constexpr char hex[] = "0123456789abcdef";
        for (size_t index = 0; index < 32; index++) {
            fingerprint += hex[hash[index] >> 4];
            fingerprint += hex[hash[index] & 15];
        }
        if (state->config.hostKey != fingerprint) {
            throw Failure("hostkey", state->config.hostKey.empty() ? "SSH host key requires approval" :
                "SSH host key changed", fingerprint);
        }
        if (retry([&] { return libssh2_userauth_password(session, state->config.username.c_str(),
                state->config.password.c_str()); }) != 0) {
            throw Failure("auth", "SSH authentication failed");
        }
        libssh2_keepalive_config(session, 1, 30);
    }
};
struct SshRelay {
    Descriptor socketFd;
    LIBSSH2_CHANNEL* channel;
    std::vector<uint8_t> toChannel, toSocket;
    std::unique_ptr<ConnectionSlot> slot;
    bool localEof = false, sentEof = false, remoteEof = false, socketEof = false;
    bool closing = false, writePending = false;
    Clock::time_point closeStarted;
    SshRelay(int fd, LIBSSH2_CHANNEL* channel) : socketFd(fd), channel(channel) {}
    // An interrupted relay is still owned by its SSH session. Session teardown
    // happens before these buffers disappear; normal close is retried in tick().
    void beginClose() {
        if (closing) return;
        closing = true;
        closeStarted = Clock::now();
        if (socketFd.fd >= 0) {
            shutdown(socketFd.fd, SHUT_RDWR);
            close(socketFd.release());
        }
        toSocket.clear();
        if (!writePending) toChannel.clear();
    }
    bool closeTick() {
        if (!channel) return false;
        if (Clock::now() - closeStarted > std::chrono::seconds(10)) {
            throw Failure("network", "SSH channel close timed out");
        }
        if (writePending) {
            const auto count = libssh2_channel_write(channel, reinterpret_cast<const char*>(toChannel.data()), toChannel.size());
            if (count == LIBSSH2_ERROR_EAGAIN) {
                char discard[16384];
                libssh2_channel_read(channel, discard, sizeof(discard));
                return true;
            }
            writePending = false;
            toChannel.clear();
        }
        const int code = libssh2_channel_free(channel);
        if (code == LIBSSH2_ERROR_EAGAIN) {
            char discard[16384];
            libssh2_channel_read(channel, discard, sizeof(discard));
            return true;
        }
        if (code != 0) throw Failure("network", "SSH channel release failed");
        channel = nullptr;
        return false;
    }
    bool tick() {
        if (closing) return closeTick();
        uint8_t buffer[16384];
        for (int iteration = 0; iteration < 8; iteration++) {
            // libssh2 requires an EAGAIN retry to retain the same buffer and length.
            if (!localEof && toChannel.empty()) {
                const auto count = recv(socketFd.fd, buffer, std::min(sizeof(buffer), MAX_BUFFER - toChannel.size()), 0);
                if (count > 0) toChannel.insert(toChannel.end(), buffer, buffer + count);
                else if (count == 0) localEof = true;
                else if (errno != EAGAIN && errno != EWOULDBLOCK && errno != EINTR) {
                    beginClose(); return closeTick();
                }
            }
            if (!toChannel.empty()) {
                const auto count = libssh2_channel_write(channel, reinterpret_cast<const char*>(toChannel.data()), toChannel.size());
                writePending = count == LIBSSH2_ERROR_EAGAIN;
                if (count > 0) Erase(toChannel, count);
                else if (!writePending) { beginClose(); return closeTick(); }
            }
            if (localEof && !sentEof && toChannel.empty()) {
                const int code = libssh2_channel_send_eof(channel);
                if (code == 0) sentEof = true;
                else if (code != LIBSSH2_ERROR_EAGAIN) { beginClose(); return closeTick(); }
            }
            if (!remoteEof && toSocket.size() < MAX_BUFFER) {
                const auto count = libssh2_channel_read(channel, reinterpret_cast<char*>(buffer),
                    std::min(sizeof(buffer), MAX_BUFFER - toSocket.size()));
                if (count > 0) toSocket.insert(toSocket.end(), buffer, buffer + count);
                else if (count == 0) remoteEof = libssh2_channel_eof(channel) != 0;
                else if (count != LIBSSH2_ERROR_EAGAIN) { beginClose(); return closeTick(); }
            }
            if (!toSocket.empty()) {
                const auto count = send(socketFd.fd, toSocket.data(), toSocket.size(), MSG_NOSIGNAL);
                if (count > 0) Erase(toSocket, count);
                else if (errno != EAGAIN && errno != EWOULDBLOCK && errno != EINTR) {
                    beginClose(); return closeTick();
                }
            }
            if (remoteEof && toSocket.empty() && !socketEof) {
                shutdown(socketFd.fd, SHUT_WR);
                socketEof = true;
            }
        }
        if (sentEof && socketEof) { beginClose(); return closeTick(); }
        return true;
    }
};
struct SshSessionEnd {
    Ssh& ssh;
    SshRelay* relay = nullptr;
    LIBSSH2_LISTENER* listener = nullptr;
    ~SshSessionEnd() { ssh.finish(relay ? relay->channel : nullptr, listener); }
};
struct PendingSshDial {
    LIBSSH2_CHANNEL* channel;
    std::shared_ptr<PendingDial> dial;
};
struct PendingH2Dial {
    std::string token;
    std::shared_ptr<PendingDial> dial;
};

struct H2Stream {
    int id = 0;
    Descriptor socketFd;
    bool control = false, probe = false, remoteEof = false, socketEof = false, localEof = false;
    bool closing = false, closed = false;
    int status = 0;
    std::string outgoing, incoming;
    std::vector<uint8_t> toSocket;
    std::unique_ptr<ConnectionSlot> slot;
    size_t probeBytes = 0;
    Clock::time_point openedAt = Clock::now();
    explicit H2Stream(int fd = -1) : socketFd(fd) {}
};
class H2 {
public:
    std::shared_ptr<State> state;
    std::shared_ptr<Mapping> mapping;
    Descriptor socketFd;
    nghttp2_session* session = nullptr;
    std::unordered_map<int32_t, std::unique_ptr<H2Stream>> streams;
    std::deque<std::string> claims;
    bool claimQueueOverflow = false;
    std::function<void(const Json&)> controlMessage;
    mbedtls_ssl_context ssl;
    mbedtls_ssl_config sslConfig;
    mbedtls_x509_crt certificates;
    mbedtls_entropy_context entropy;
    mbedtls_ctr_drbg_context random;
    bool tls = false;
    explicit H2(const std::shared_ptr<State>& state, const std::shared_ptr<Mapping>& mapping = nullptr)
        : H2(state, mapping, Dial(state->config.host, state->config.port, state, mapping)) {}
    H2(const std::shared_ptr<State>& state, const std::shared_ptr<Mapping>& mapping, int connectedSocket)
        : state(state), mapping(mapping), socketFd(connectedSocket) {
        mbedtls_ssl_init(&ssl);
        mbedtls_ssl_config_init(&sslConfig);
        mbedtls_x509_crt_init(&certificates);
        mbedtls_entropy_init(&entropy);
        mbedtls_ctr_drbg_init(&random);
    }
    ~H2() {
        if (session) nghttp2_session_del(session);
        mbedtls_ssl_free(&ssl);
        mbedtls_ssl_config_free(&sslConfig);
        mbedtls_x509_crt_free(&certificates);
        mbedtls_ctr_drbg_free(&random);
        mbedtls_entropy_free(&entropy);
    }
    void initialize() {
        if (state->config.tls) {
            static constexpr unsigned char personalization[] = "ClawBench-H2";
            if (mbedtls_ctr_drbg_seed(&random, mbedtls_entropy_func, &entropy, personalization,
                    sizeof(personalization) - 1) != 0 ||
                mbedtls_x509_crt_parse(&certificates, reinterpret_cast<const unsigned char*>(CLAWBENCH_CA_BUNDLE),
                    sizeof(CLAWBENCH_CA_BUNDLE)) < 0 ||
                mbedtls_ssl_config_defaults(&sslConfig, MBEDTLS_SSL_IS_CLIENT, MBEDTLS_SSL_TRANSPORT_STREAM,
                    MBEDTLS_SSL_PRESET_DEFAULT) != 0) {
                throw Failure("network", "TLS configuration failed");
            }
            mbedtls_ssl_conf_authmode(&sslConfig, MBEDTLS_SSL_VERIFY_REQUIRED);
            mbedtls_ssl_conf_ca_chain(&sslConfig, &certificates, nullptr);
            mbedtls_ssl_conf_rng(&sslConfig, mbedtls_ctr_drbg_random, &random);
            static const char* protocols[] = {"h2", nullptr};
            if (mbedtls_ssl_conf_alpn_protocols(&sslConfig, protocols) != 0 || mbedtls_ssl_setup(&ssl, &sslConfig) != 0 ||
                mbedtls_ssl_set_hostname(&ssl, state->config.host.c_str()) != 0) {
                throw Failure("network", "TLS setup failed");
            }
            mbedtls_ssl_set_bio(&ssl, this, sendTls, receiveTls, nullptr);
            const auto deadline = Clock::now() + std::chrono::seconds(15);
            int code;
            while ((code = mbedtls_ssl_handshake(&ssl)) != 0) {
                CheckAlive(state, mapping);
                if (code != MBEDTLS_ERR_SSL_WANT_READ && code != MBEDTLS_ERR_SSL_WANT_WRITE) {
                    throw Failure("certificate", "TLS certificate or handshake rejected");
                }
                if (Clock::now() >= deadline) throw Failure("network", "TLS handshake timed out");
                pollfd item{socketFd.fd, static_cast<short>(code == MBEDTLS_ERR_SSL_WANT_WRITE ? POLLOUT : POLLIN), 0};
                poll(&item, 1, 100);
            }
            const char* protocol = mbedtls_ssl_get_alpn_protocol(&ssl);
            if (!protocol || std::strcmp(protocol, "h2") != 0) throw Failure("network", "Server did not negotiate HTTP/2");
            tls = true;
        }
        nghttp2_session_callbacks* callbacks = nullptr;
        nghttp2_session_callbacks_new(&callbacks);
        nghttp2_session_callbacks_set_send_callback(callbacks, sendH2);
        nghttp2_session_callbacks_set_recv_callback(callbacks, receiveH2);
        nghttp2_session_callbacks_set_on_header_callback(callbacks, header);
        nghttp2_session_callbacks_set_on_data_chunk_recv_callback(callbacks, data);
        nghttp2_session_callbacks_set_on_frame_recv_callback(callbacks, frame);
        nghttp2_session_callbacks_set_on_stream_close_callback(callbacks, closed);
        nghttp2_option* options = nullptr;
        nghttp2_option_new(&options);
        nghttp2_option_set_no_auto_window_update(options, 1);
        const int result = nghttp2_session_client_new2(&session, callbacks, this, options);
        nghttp2_option_del(options);
        nghttp2_session_callbacks_del(callbacks);
        if (result != 0) throw Failure("network", "HTTP/2 allocation failed");
        nghttp2_settings_entry settings[] = {{NGHTTP2_SETTINGS_ENABLE_PUSH, 0},
            {NGHTTP2_SETTINGS_INITIAL_WINDOW_SIZE, static_cast<uint32_t>(MAX_BUFFER)}};
        nghttp2_submit_settings(session, NGHTTP2_FLAG_NONE, settings, 2);
        // Leave connection credit for every bounded stream and for the control heartbeat.
        if (nghttp2_session_set_local_window_size(session, NGHTTP2_FLAG_NONE, 0,
                static_cast<int32_t>(MAX_BUFFER * (MAX_CONNECTIONS + 1))) != 0) {
            throw Failure("network", "HTTP/2 flow control setup failed");
        }
    }
    int open(const std::string& path, std::unique_ptr<H2Stream> stream, bool get = false) {
        std::vector<std::pair<std::string, std::string>> values{
            {":method", get ? "GET" : "POST"}, {":scheme", state->config.tls ? "https" : "http"},
            {":authority", state->config.authority}, {":path", path},
            {"content-type", "application/octet-stream"}, {"cookie", state->config.cookie}
        };
        std::vector<nghttp2_nv> headers;
        for (auto& value : values) {
            headers.push_back({reinterpret_cast<uint8_t*>(value.first.data()), reinterpret_cast<uint8_t*>(value.second.data()),
                value.first.size(), value.second.size(), NGHTTP2_NV_FLAG_NONE});
        }
        nghttp2_data_provider provider{};
        provider.source.ptr = stream.get();
        provider.read_callback = readBody;
        const int id = nghttp2_submit_request(session, nullptr, headers.data(), headers.size(),
            get ? nullptr : &provider, stream.get());
        if (id < 0) throw Failure("network", "HTTP/2 stream could not be opened");
        stream->id = id;
        streams.emplace(id, std::move(stream));
        return id;
    }
    bool queueClaim(const std::string& token) {
        if (claims.size() >= MAX_CONNECTIONS) {
            claimQueueOverflow = true;
            return false;
        }
        claims.push_back(token);
        return true;
    }
    void pump(int milliseconds = 30) {
        CheckAlive(state, mapping);
        for (auto& pair : streams) {
            H2Stream& stream = *pair.second;
            if (stream.closing) continue;
            if (!stream.closed && stream.status == 0 &&
                Clock::now() - stream.openedAt > std::chrono::seconds(15)) {
                throw Failure("network", "HTTP/2 response timed out");
            }
            if (stream.socketFd.fd >= 0 && !stream.toSocket.empty()) {
                const auto count = send(stream.socketFd.fd, stream.toSocket.data(), stream.toSocket.size(), MSG_NOSIGNAL);
                if (count > 0) {
                    Erase(stream.toSocket, count);
                    nghttp2_session_consume(session, stream.id, count);
                } else if (count == 0 || (errno != EAGAIN && errno != EWOULDBLOCK && errno != EINTR)) {
                    if (reset(session, stream) != 0) throw Failure("network", "HTTP/2 stream reset failed");
                    continue;
                }
            }
            if (stream.remoteEof && stream.toSocket.empty() && stream.socketFd.fd >= 0 && !stream.socketEof) {
                shutdown(stream.socketFd.fd, SHUT_WR);
                stream.socketEof = true;
            }
            if (!stream.closed) nghttp2_session_resume_data(session, stream.id);
        }
        if (nghttp2_session_send(session) < 0 || nghttp2_session_recv(session) < 0) {
            throw Failure("network", "HTTP/2 connection closed");
        }
        std::vector<pollfd> descriptors{{socketFd.fd, POLLIN, 0}};
        if (nghttp2_session_want_write(session)) descriptors[0].events |= POLLOUT;
        for (auto& pair : streams) {
            auto& stream = *pair.second;
            if (stream.socketFd.fd < 0 || stream.closing || stream.closed) continue;
            short events = stream.localEof || nghttp2_session_get_remote_window_size(session) <= 0 ||
                nghttp2_session_get_stream_remote_window_size(session, stream.id) <= 0 ? 0 : POLLIN;
            if (!stream.toSocket.empty()) events |= POLLOUT;
            if (events) descriptors.push_back({stream.socketFd.fd, events, 0});
        }
        poll(descriptors.data(), descriptors.size(), milliseconds);
    }
    void probe() {
        auto stream = std::make_unique<H2Stream>();
        stream->probe = true;
        const int id = open("/api/ssh/info/full", std::move(stream), true);
        const auto deadline = Clock::now() + std::chrono::seconds(15);
        while (streams[id]->status == 0 && !streams[id]->closed && Clock::now() < deadline) pump();
        const int status = streams[id]->status;
        reset(session, *streams[id]);
        if (status == 401 || status == 403) throw Failure("auth", "Tunnel authentication rejected");
        if (status != 200) throw Failure("network", "Server did not accept HTTP/2 tunnel probe");
    }
private:
    static void discard(nghttp2_session* session, H2Stream& stream) {
        if (!stream.toSocket.empty()) {
            nghttp2_session_consume(session, stream.id, stream.toSocket.size());
            stream.toSocket.clear();
        }
        if (stream.socketFd.fd >= 0) close(stream.socketFd.release());
    }
    static int reset(nghttp2_session* session, H2Stream& stream) {
        discard(session, stream);
        if (stream.closing || stream.closed) return 0;
        stream.closing = true;
        // The data provider and user_data stay alive until on_stream_close.
        return nghttp2_submit_rst_stream(session, NGHTTP2_FLAG_NONE, stream.id, NGHTTP2_CANCEL) == 0 ?
            0 : NGHTTP2_ERR_CALLBACK_FAILURE;
    }
    static int sendTls(void* context, const unsigned char* bytes, size_t length) {
        auto* self = static_cast<H2*>(context);
        const auto count = send(self->socketFd.fd, bytes, length, MSG_NOSIGNAL);
        if (count >= 0) return static_cast<int>(count);
        return errno == EAGAIN || errno == EWOULDBLOCK || errno == EINTR ? MBEDTLS_ERR_SSL_WANT_WRITE : MBEDTLS_ERR_SSL_INTERNAL_ERROR;
    }
    static int receiveTls(void* context, unsigned char* bytes, size_t length) {
        auto* self = static_cast<H2*>(context);
        const auto count = recv(self->socketFd.fd, bytes, length, 0);
        if (count >= 0) return static_cast<int>(count);
        return errno == EAGAIN || errno == EWOULDBLOCK || errno == EINTR ? MBEDTLS_ERR_SSL_WANT_READ : MBEDTLS_ERR_SSL_INTERNAL_ERROR;
    }
    static ssize_t sendH2(nghttp2_session*, const uint8_t* bytes, size_t length, int, void* context) {
        auto* self = static_cast<H2*>(context);
        const auto count = self->tls ? mbedtls_ssl_write(&self->ssl, bytes, length) :
            sendTls(self, bytes, length);
        if (count >= 0) return count;
        return count == MBEDTLS_ERR_SSL_WANT_WRITE || count == MBEDTLS_ERR_SSL_WANT_READ ?
            NGHTTP2_ERR_WOULDBLOCK : NGHTTP2_ERR_CALLBACK_FAILURE;
    }
    static ssize_t receiveH2(nghttp2_session*, uint8_t* bytes, size_t length, int, void* context) {
        auto* self = static_cast<H2*>(context);
        const auto count = self->tls ? mbedtls_ssl_read(&self->ssl, bytes, length) :
            receiveTls(self, bytes, length);
        if (count > 0) return count;
        if (count == 0 || count == MBEDTLS_ERR_SSL_PEER_CLOSE_NOTIFY) return NGHTTP2_ERR_EOF;
        return count == MBEDTLS_ERR_SSL_WANT_READ || count == MBEDTLS_ERR_SSL_WANT_WRITE ?
            NGHTTP2_ERR_WOULDBLOCK : NGHTTP2_ERR_CALLBACK_FAILURE;
    }
    static ssize_t readBody(nghttp2_session*, int32_t, uint8_t* bytes, size_t length, uint32_t* flags,
        nghttp2_data_source* source, void*) {
        auto* stream = static_cast<H2Stream*>(source->ptr);
        if (stream->closing || stream->closed) return NGHTTP2_ERR_DEFERRED;
        if (stream->control) {
            if (stream->outgoing.empty()) return NGHTTP2_ERR_DEFERRED;
            const size_t size = std::min(length, stream->outgoing.size());
            std::memcpy(bytes, stream->outgoing.data(), size);
            stream->outgoing.erase(0, size);
            return size;
        }
        const auto count = recv(stream->socketFd.fd, bytes, length, 0);
        if (count > 0) return count;
        if (count == 0) {
            stream->localEof = true;
            *flags |= NGHTTP2_DATA_FLAG_EOF;
            return 0;
        }
        return errno == EAGAIN || errno == EWOULDBLOCK || errno == EINTR ?
            NGHTTP2_ERR_DEFERRED : NGHTTP2_ERR_TEMPORAL_CALLBACK_FAILURE;
    }
    static int header(nghttp2_session* session, const nghttp2_frame* frame, const uint8_t* name, size_t nameLength,
        const uint8_t* value, size_t valueLength, uint8_t, void*) {
        auto* stream = static_cast<H2Stream*>(nghttp2_session_get_stream_user_data(session, frame->hd.stream_id));
        if (stream && std::string(reinterpret_cast<const char*>(name), nameLength) == ":status") {
            const std::string status(reinterpret_cast<const char*>(value), valueLength);
            if (status.size() == 3 && std::all_of(status.begin(), status.end(), ::isdigit)) stream->status = std::stoi(status);
        }
        return 0;
    }
    static int data(nghttp2_session* session, uint8_t, int32_t id, const uint8_t* bytes, size_t length, void* context) {
        auto* self = static_cast<H2*>(context);
        auto* stream = static_cast<H2Stream*>(nghttp2_session_get_stream_user_data(session, id));
        if (!stream) return 0;
        if (stream->closing || stream->closed) {
            nghttp2_session_consume(session, id, length);
            return 0;
        }
        if (stream->probe) {
            if (length > 65536 - stream->probeBytes) return NGHTTP2_ERR_CALLBACK_FAILURE;
            stream->probeBytes += length;
            nghttp2_session_consume(session, id, length);
            return 0;
        }
        if (stream->control) {
            stream->incoming.append(reinterpret_cast<const char*>(bytes), length);
            if (stream->incoming.size() > 65536) return NGHTTP2_ERR_TEMPORAL_CALLBACK_FAILURE;
            size_t newline;
            while ((newline = stream->incoming.find('\n')) != std::string::npos) {
                const std::string line = stream->incoming.substr(0, newline);
                stream->incoming.erase(0, newline + 1);
                try {
                    Json message = Json::parse(line);
                    if (self->controlMessage) self->controlMessage(message);
                } catch (...) { return NGHTTP2_ERR_TEMPORAL_CALLBACK_FAILURE; }
            }
            nghttp2_session_consume(session, id, length);
        } else {
            if (stream->toSocket.size() + length > MAX_BUFFER) return NGHTTP2_ERR_TEMPORAL_CALLBACK_FAILURE;
            stream->toSocket.insert(stream->toSocket.end(), bytes, bytes + length);
        }
        return 0;
    }
    static int frame(nghttp2_session* session, const nghttp2_frame* frame, void* context) {
        if (frame->hd.type == NGHTTP2_GOAWAY) return NGHTTP2_ERR_CALLBACK_FAILURE;
        auto* stream = static_cast<H2Stream*>(nghttp2_session_get_stream_user_data(session, frame->hd.stream_id));
        if (!stream) return 0;
        if ((frame->hd.type == NGHTTP2_DATA || frame->hd.type == NGHTTP2_HEADERS) && (frame->hd.flags & NGHTTP2_FLAG_END_STREAM)) {
            stream->remoteEof = true;
        }
        if (frame->hd.type == NGHTTP2_HEADERS && stream->status >= 300) {
            if (stream->status == 401) {
                static_cast<H2*>(context)->state->fail("auth", "Tunnel authentication rejected");
            }
            return reset(session, *stream);
        }
        return 0;
    }
    static int closed(nghttp2_session* session, int32_t id, uint32_t error, void*) {
        auto* stream = static_cast<H2Stream*>(nghttp2_session_get_stream_user_data(session, id));
        if (stream) {
            stream->closed = true;
            stream->remoteEof = true;
            if (error != NGHTTP2_NO_ERROR) discard(session, *stream);
        }
        return 0;
    }
};

void ReapH2Streams(H2& h2, int control)
{
    for (auto it = h2.streams.begin(); it != h2.streams.end();) {
        if (it->first != control && it->second->closed && it->second->toSocket.empty()) it = h2.streams.erase(it);
        else ++it;
    }
}
bool StartNextH2Dial(H2& h2, std::vector<PendingH2Dial>& pending)
{
    if (h2.claims.empty() || h2.streams.size() + pending.size() >= MAX_CONNECTIONS + 1) return false;
    try {
        auto dial = StartDial(h2.state, h2.mapping);
        pending.push_back({h2.claims.front(), std::move(dial)});
        h2.claims.pop_front();
        return true;
    } catch (const Failure& error) {
        // A server relay can remain open briefly after its visitor disconnects.
        // Keep its successor queued until both local and global capacity are free.
        if (error.type == "capacity") return false;
        throw;
    }
}

void ForwardConnection(const std::shared_ptr<State>& state, const std::shared_ptr<Mapping>& mapping, int fd)
{
    Descriptor client(fd);
    try {
        ConnectionSlot slot(state);
        if (state->config.kind == "ssh") {
            Ssh ssh(state, mapping);
            ssh.authenticate();
            LIBSSH2_CHANNEL* channel = nullptr;
            if (ssh.retry([&] {
                channel = libssh2_channel_direct_tcpip_ex(ssh.session, mapping->host.c_str(), mapping->target, "127.0.0.1", 0);
                return channel ? 0 : libssh2_session_last_errno(ssh.session);
            }) != 0) return;
            SshRelay relay(client.release(), channel);
            SshSessionEnd finish{ssh, &relay, nullptr};
            while (Alive(state, mapping) && relay.tick()) ssh.wait(20);
        } else {
            H2 h2(state, mapping);
            h2.initialize();
            const std::string path = "/api/tunnel/stream?host=" + mapping->host + "&port=" + std::to_string(mapping->target);
            const int id = h2.open(path, std::make_unique<H2Stream>(client.release()));
            while (Alive(state, mapping) && !(h2.streams[id]->closed && h2.streams[id]->toSocket.empty())) h2.pump();
        }
    } catch (const Failure& error) {
        if (Alive(state, mapping) && (error.type == "auth" || error.type == "hostkey" || error.type == "certificate")) {
            state->fail(error.type, error.what());
        }
    } catch (...) {}
}
void ForwardLoop(const std::shared_ptr<State>& state, const std::shared_ptr<Mapping>& mapping, int fd)
{
    Descriptor listener(fd);
    while (Alive(state, mapping)) {
        pollfd wait{fd, POLLIN, 0};
        if (poll(&wait, 1, 100) <= 0) continue;
        const int accepted = accept(fd, nullptr, nullptr);
        if (accepted < 0) continue;
        try {
            NonBlocking(accepted);
            std::thread(ForwardConnection, state, mapping, accepted).detach();
        } catch (...) { close(accepted); }
    }
}
void ReverseSsh(const std::shared_ptr<State>& state, const std::shared_ptr<Mapping>& mapping)
{
    try {
        Ssh ssh(state, mapping);
        ssh.authenticate();
        LIBSSH2_LISTENER* listener = nullptr;
        int actual = mapping->port;
        if (ssh.retry([&] {
            listener = libssh2_channel_forward_listen_ex(ssh.session, "127.0.0.1", mapping->port, &actual, 32);
            return listener ? 0 : libssh2_session_last_errno(ssh.session);
        }) != 0 || actual != mapping->port) {
            mapping->complete(false, "conflict");
            return;
        }
        mapping->complete(true);
        std::vector<std::unique_ptr<SshRelay>> relays;
        std::vector<PendingSshDial> pending;
        relays.reserve(MAX_CONNECTIONS);
        pending.reserve(MAX_CONNECTIONS);
        SshSessionEnd finish{ssh, nullptr, listener};
        const auto closeChannel = [&](LIBSSH2_CHANNEL* channel) {
            auto relay = std::make_unique<SshRelay>(-1, channel);
            relay->beginClose();
            relays.push_back(std::move(relay));
        };
        while (Alive(state, mapping)) {
            if (relays.size() + pending.size() < MAX_CONNECTIONS) {
                auto* channel = libssh2_channel_forward_accept(listener);
                if (channel) {
                    try { pending.push_back({channel, StartDial(state, mapping)}); }
                    catch (...) { closeChannel(channel); }
                } else if (libssh2_session_last_errno(ssh.session) != LIBSSH2_ERROR_EAGAIN) {
                    throw Failure("network", "SSH reverse listener disconnected");
                }
            }
            for (auto it = pending.begin(); it != pending.end();) {
                if (!it->dial->done.load(std::memory_order_acquire)) { ++it; continue; }
                auto relay = std::make_unique<SshRelay>(-1, it->channel);
                relay->socketFd.fd = it->dial->socketFd.release();
                relay->slot = std::move(it->dial->slot);
                if (relay->socketFd.fd < 0) relay->beginClose();
                relays.push_back(std::move(relay));
                it = pending.erase(it);
            }
            relays.erase(std::remove_if(relays.begin(), relays.end(), [](const auto& relay) { return !relay->tick(); }), relays.end());
            int seconds = 0;
            const int heartbeat = libssh2_keepalive_send(ssh.session, &seconds);
            if (heartbeat != 0 && heartbeat != LIBSSH2_ERROR_EAGAIN) throw Failure("network", "SSH keepalive failed");
            ssh.wait(20);
        }
    } catch (const Failure& error) {
        mapping->complete(false, error.type);
        if (Alive(state, mapping)) state->fail(error.type, error.what());
    } catch (...) {
        mapping->complete(false, "network");
        if (Alive(state, mapping)) state->fail("network", "Reverse tunnel failed");
    }
}
void ReverseH2(const std::shared_ptr<State>& state, const std::shared_ptr<Mapping>& mapping)
{
    try {
        H2 h2(state, mapping);
        h2.initialize();
        auto stream = std::make_unique<H2Stream>();
        stream->control = true;
        stream->outgoing = Json{{"type", "bind"}, {"port", mapping->port}}.dump() + "\n";
        const int control = h2.open("/api/tunnel/control", std::move(stream));
        std::vector<PendingH2Dial> pending;
        pending.reserve(MAX_CONNECTIONS);
        auto lastPong = Clock::now();
        auto lastPing = Clock::now();
        const auto bindDeadline = Clock::now() + std::chrono::seconds(15);
        h2.controlMessage = [&](const Json& message) {
            const std::string type = message.value("type", "");
            const int port = message.value("port", 0);
            if (type == "bound" && port == mapping->port) mapping->complete(true);
            else if (type == "bind_err" && port == mapping->port) {
                mapping->complete(false, message.value("code", 0) == 3 ? "conflict" : "unreachable");
                mapping->live = false;
            } else if (type == "incoming" && port == mapping->port) {
                const std::string token = message.value("token", "");
                if (token.size() <= 256 && std::regex_match(token, std::regex("[A-Za-z0-9_-]+"))) h2.queueClaim(token);
            } else if (type == "pong") lastPong = Clock::now();
        };
        while (Alive(state, mapping)) {
            h2.pump();
            if (h2.streams[control]->closed) throw Failure("network", "HTTP/2 control stream closed");
            if (h2.claimQueueOverflow) throw Failure("capacity", "Reverse tunnel incoming queue is full");
            {
                std::lock_guard<std::mutex> lock(mapping->mutex);
                if (!mapping->settled && Clock::now() > bindDeadline) throw Failure("network", "Reverse bind timed out");
            }
            ReapH2Streams(h2, control);
            for (auto it = pending.begin(); it != pending.end();) {
                if (!it->dial->done.load(std::memory_order_acquire)) { ++it; continue; }
                if (it->dial->socketFd.fd >= 0) {
                    auto stream = std::make_unique<H2Stream>();
                    stream->socketFd.fd = it->dial->socketFd.release();
                    stream->slot = std::move(it->dial->slot);
                    h2.open("/api/tunnel/stream?claim=" + it->token, std::move(stream));
                } else {
                    // Closing the control session also closes the unclaimed visitor.
                    throw Failure("network", "Reverse tunnel target could not be reached");
                }
                it = pending.erase(it);
            }
            while (StartNextH2Dial(h2, pending)) {}
            if (Clock::now() - lastPing > std::chrono::seconds(30)) {
                h2.streams[control]->outgoing += "{\"type\":\"ping\"}\n";
                lastPing = Clock::now();
            }
            if (Clock::now() - lastPong > std::chrono::seconds(65)) throw Failure("network", "HTTP/2 keepalive timed out");
        }
    } catch (const Failure& error) {
        mapping->complete(false, error.type);
        if (Alive(state, mapping)) state->fail(error.type, error.what());
    } catch (...) {
        mapping->complete(false, "network");
        if (Alive(state, mapping)) state->fail("network", "Reverse tunnel failed");
    }
}
Json Snapshot(const std::shared_ptr<State>& state)
{
    if (!state) return {{"ok", true}, {"connected", false}, {"error", ""}, {"errorType", ""}};
    std::lock_guard<std::mutex> lock(state->mutex);
    return {{"ok", true}, {"connected", state->connected.load() && state->live.load()},
        {"error", state->error}, {"errorType", state->errorType}, {"transport", state->config.kind}};
}
void Monitor(const std::shared_ptr<State>& state)
{
    while (state->live) {
        for (int tick = 0; tick < 150 && state->live; tick++) std::this_thread::sleep_for(std::chrono::milliseconds(100));
        if (!state->live) break;
        try {
            if (state->config.kind == "ssh") { Ssh ssh(state); ssh.authenticate(); }
            else { H2 h2(state); h2.initialize(); h2.probe(); }
        } catch (const Failure& error) {
            if (state->live) state->fail(error.type, error.what());
        } catch (...) {
            if (state->live) state->fail("network", "Tunnel connection lost");
        }
    }
}
Json Run(const Json& command, uint64_t epoch)
{
    const std::string method = command.value("method", "");
    std::shared_ptr<State> state;
    {
        std::lock_guard<std::mutex> lock(currentMutex);
        state = current;
    }
    if (method == "status") return Snapshot(state);
    if (method == "disconnect") { CancelTunnel(); return {{"ok", true}}; }
    if (method == "connect") {
        auto next = std::make_shared<State>();
        next->config = ParseConfig(command);
        {
            std::lock_guard<std::mutex> lock(currentMutex);
            if (cancelEpoch.load() != epoch) throw Failure("network", "Tunnel cancelled");
            Stop(current);
            state = next;
            current = state;
        }
        try {
            if (state->config.kind == "ssh") { Ssh ssh(state); ssh.authenticate(); }
            else { H2 h2(state); h2.initialize(); h2.probe(); }
            CheckAlive(state);
            state->connected = true;
            std::thread(Monitor, state).detach();
            return Snapshot(state);
        } catch (...) {
            state->live = false;
            throw;
        }
    }
    if (!state || !state->live || !state->connected) throw Failure("network", "Tunnel is not connected");
    const int port = Port(command, "port");
    const std::string direction = command.value("direction", "forward");
    if (direction != "forward" && direction != "reverse") throw Failure("invalid", "Invalid tunnel direction");
    const std::string key = direction + ":" + std::to_string(port);
    if (method == "probe") {
        std::shared_ptr<Mapping> mapping;
        {
            std::lock_guard<std::mutex> lock(state->mutex);
            const auto found = state->mappings.find("forward:" + std::to_string(port));
            if (found != state->mappings.end() && found->second->live) mapping = found->second;
        }
        if (!mapping) return {{"ok", true}, {"reachable", false}};
        try {
            Descriptor connection(Dial("127.0.0.1", port, state, mapping));
            return {{"ok", true}, {"reachable", true}};
        } catch (...) { return {{"ok", true}, {"reachable", false}}; }
    }
    if (method == "remove") {
        std::lock_guard<std::mutex> lock(state->mutex);
        const auto found = state->mappings.find(key);
        if (found != state->mappings.end()) {
            found->second->live = false;
            state->mappings.erase(found);
        }
        return {{"ok", true}};
    }
    if (method != "add") throw Failure("invalid", "Unknown tunnel operation");
    auto mapping = std::make_shared<Mapping>();
    mapping->port = port;
    mapping->target = Port(command, "targetPort");
    mapping->host = Host(command.value("host", ""));
    mapping->direction = direction;
    {
        std::lock_guard<std::mutex> lock(state->mutex);
        const auto previous = state->mappings.find(key);
        if (previous != state->mappings.end()) {
            if (previous->second->live && previous->second->host == mapping->host &&
                previous->second->target == mapping->target) return {{"ok", true}, {"port", port}};
            return {{"ok", false}, {"reason", "conflict"}};
        }
        if (state->mappings.size() >= MAX_MAPPINGS) throw Failure("invalid", "Too many port mappings");
    }
    if (direction == "forward") {
        Descriptor listener(socket(AF_INET, SOCK_STREAM, 0));
        if (listener.fd < 0) throw Failure("network", "Local listener allocation failed");
        NonBlocking(listener.fd);
        sockaddr_in address{};
        address.sin_family = AF_INET;
        address.sin_addr.s_addr = htonl(INADDR_LOOPBACK);
        bool bound = false;
        for (int candidate = port; candidate <= std::min(port + 50, 65535); candidate++) {
            address.sin_port = htons(static_cast<uint16_t>(candidate));
            if (bind(listener.fd, reinterpret_cast<sockaddr*>(&address), sizeof(address)) == 0) {
                mapping->port = candidate; bound = true; break;
            }
            if (errno != EADDRINUSE) throw Failure("network", "Local listener bind failed");
        }
        if (!bound) return {{"ok", false}, {"reason", "conflict"}};
        if (listen(listener.fd, 32) != 0) throw Failure("network", "Local listener failed");
        {
            std::lock_guard<std::mutex> lock(state->mutex);
            state->mappings[direction + ":" + std::to_string(mapping->port)] = mapping;
        }
        const int fd = listener.release();
        std::thread(ForwardLoop, state, mapping, fd).detach();
        return {{"ok", true}, {"port", mapping->port}};
    }
    {
        std::lock_guard<std::mutex> lock(state->mutex);
        state->mappings[key] = mapping;
    }
    std::thread(state->config.kind == "ssh" ? ReverseSsh : ReverseH2, state, mapping).detach();
    std::string reason;
    {
        std::unique_lock<std::mutex> lock(mapping->mutex);
        mapping->ready.wait_for(lock, std::chrono::seconds(20), [&] { return mapping->settled || !state->live; });
        if (mapping->settled && mapping->ok && Alive(state, mapping)) return {{"ok", true}, {"port", port}};
        mapping->live = false;
        reason = mapping->reason;
    }
    {
        std::lock_guard<std::mutex> lock(state->mutex);
        state->mappings.erase(key);
    }
    return {{"ok", false}, {"reason", reason == "conflict" ? "conflict" : "unreachable"}};
}
}

void CancelTunnel()
{
    std::lock_guard<std::mutex> lock(currentMutex);
    ++cancelEpoch;
    Stop(current);
}
uint64_t TunnelEpoch() { return cancelEpoch.load(); }
std::string ExecuteTunnel(const std::string& command)
{
    return ExecuteTunnel(command, TunnelEpoch());
}
std::string ExecuteTunnel(const std::string& command, uint64_t epoch)
{
    std::lock_guard<std::mutex> lock(commandMutex);
    try {
        if (cancelEpoch.load() != epoch) throw Failure("network", "Tunnel cancelled");
        std::call_once(initFlag, [] { if (libssh2_init(0) != 0) throw Failure("network", "SSH initialization failed"); });
        if (command.size() > 65536) throw Failure("invalid", "Tunnel command too large");
        return Run(Json::parse(command), epoch).dump();
    } catch (const Failure& error) {
        return Json{{"ok", false}, {"error", error.what()}, {"errorType", error.type},
            {"fingerprint", error.fingerprint}}.dump();
    } catch (...) {
        return Json{{"ok", false}, {"error", "Invalid or failed tunnel operation"}, {"errorType", "unknown"}}.dump();
    }
}
}
