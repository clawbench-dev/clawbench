#include <sys/socket.h>
#include <cerrno>
#include <cstddef>

static int blockedSendSocket = -1;
static int failedSendSocket = -1;
static ssize_t TestSend(int socket, const void* bytes, size_t length, int flags)
{
    if (socket == failedSendSocket) {
        errno = EPIPE;
        return -1;
    }
    if (socket == blockedSendSocket) {
        errno = EAGAIN;
        return -1;
    }
    return ::send(socket, bytes, length, flags);
}

#define send TestSend
#define libssh2_channel_write_ex CheckChannelWrite
#define libssh2_channel_read_ex CheckChannelRead
#define libssh2_channel_free CheckChannelFree
#include "../../entry/src/main/cpp/tunnel_engine.cpp"
#undef send
#undef libssh2_channel_write_ex
#undef libssh2_channel_read_ex
#undef libssh2_channel_free
#include <cassert>
#include <iostream>

static std::deque<int> writeResults, freeResults;
static const char* expectedBuffer = nullptr;
static size_t expectedLength = 0;
static int writeCalls = 0, readCalls = 0, freeCalls = 0;

extern "C" ssize_t CheckChannelWrite(LIBSSH2_CHANNEL*, int, const char* buffer, size_t length)
{
    assert(buffer == expectedBuffer && length == expectedLength && !writeResults.empty());
    ++writeCalls;
    const int result = writeResults.front();
    writeResults.pop_front();
    return result;
}
extern "C" ssize_t CheckChannelRead(LIBSSH2_CHANNEL*, int, char*, size_t)
{
    ++readCalls;
    return LIBSSH2_ERROR_EAGAIN;
}
extern "C" int CheckChannelFree(LIBSSH2_CHANNEL*)
{
    assert(!freeResults.empty());
    ++freeCalls;
    const int result = freeResults.front();
    freeResults.pop_front();
    return result;
}

namespace {
using namespace clawbench;

struct SocketPair {
    int sockets[2]{-1, -1};
    SocketPair() {
        assert(socketpair(AF_UNIX, SOCK_STREAM, 0, sockets) == 0);
        NonBlocking(sockets[0]);
        NonBlocking(sockets[1]);
    }
    ~SocketPair() {
        for (int socket : sockets) if (socket >= 0) close(socket);
    }
    int take(int index) {
        const int socket = sockets[index];
        sockets[index] = -1;
        return socket;
    }
};

class Server {
public:
    Descriptor socketFd;
    nghttp2_session* session = nullptr;
    explicit Server(int socket) : socketFd(socket) {
        nghttp2_session_callbacks* callbacks = nullptr;
        assert(nghttp2_session_callbacks_new(&callbacks) == 0);
        nghttp2_session_callbacks_set_send_callback(callbacks, sendBytes);
        nghttp2_session_callbacks_set_recv_callback(callbacks, receiveBytes);
        assert(nghttp2_session_server_new(&session, callbacks, this) == 0);
        nghttp2_session_callbacks_del(callbacks);
        assert(nghttp2_submit_settings(session, NGHTTP2_FLAG_NONE, nullptr, 0) == 0);
    }
    ~Server() { nghttp2_session_del(session); }
    void pump() {
        assert(nghttp2_session_recv(session) == 0);
        assert(nghttp2_session_send(session) == 0);
    }
    void respond(int id, const std::string& status) {
        std::string name = ":status";
        nghttp2_nv header{reinterpret_cast<uint8_t*>(name.data()), reinterpret_cast<uint8_t*>(const_cast<char*>(status.data())),
            name.size(), status.size(), NGHTTP2_NV_FLAG_NONE};
        assert(nghttp2_submit_headers(session, NGHTTP2_FLAG_NONE, id, nullptr, &header, 1, nullptr) == 0);
        assert(nghttp2_session_send(session) == 0);
    }
    void body(int id, std::string& text) {
        nghttp2_data_provider provider{};
        provider.source.ptr = &text;
        provider.read_callback = readBody;
        assert(nghttp2_submit_data(session, NGHTTP2_FLAG_NONE, id, &provider) == 0);
        assert(nghttp2_session_send(session) == 0);
    }
    void reset(int id, uint32_t error = NGHTTP2_CANCEL) {
        assert(nghttp2_submit_rst_stream(session, NGHTTP2_FLAG_NONE, id, error) == 0);
        assert(nghttp2_session_send(session) == 0);
    }
private:
    static ssize_t sendBytes(nghttp2_session*, const uint8_t* bytes, size_t length, int, void* context) {
        const auto count = ::send(static_cast<Server*>(context)->socketFd.fd, bytes, length, MSG_NOSIGNAL);
        return count >= 0 ? count : NGHTTP2_ERR_WOULDBLOCK;
    }
    static ssize_t receiveBytes(nghttp2_session*, uint8_t* bytes, size_t length, int, void* context) {
        const auto count = recv(static_cast<Server*>(context)->socketFd.fd, bytes, length, 0);
        return count >= 0 ? count : NGHTTP2_ERR_WOULDBLOCK;
    }
    static ssize_t readBody(nghttp2_session*, int32_t, uint8_t* bytes, size_t length, uint32_t* flags,
        nghttp2_data_source* source, void*) {
        auto& body = *static_cast<std::string*>(source->ptr);
        const size_t count = std::min(body.size(), length);
        std::memcpy(bytes, body.data(), count);
        body.erase(0, count);
        if (body.empty()) *flags |= NGHTTP2_DATA_FLAG_EOF | NGHTTP2_DATA_FLAG_NO_END_STREAM;
        return static_cast<ssize_t>(count);
    }
};

void CheckQueuedResetLifetime()
{
    auto state = std::make_shared<State>();
    state->config.authority = "localhost";
    SocketPair sockets;
    H2 client(state, nullptr, sockets.take(0));
    Server server(sockets.take(1));
    client.initialize();
    const int id = client.open("/reject", std::make_unique<H2Stream>(), true);
    client.pump(0);
    server.pump();
    server.respond(id, "403");
    client.pump(0);
    auto* stream = client.streams.at(id).get();
    assert(stream->closing && !stream->closed);
    assert(nghttp2_session_get_stream_user_data(client.session, id) == stream);
    ReapH2Streams(client, 0);
    assert(client.streams.count(id) == 1);
    blockedSendSocket = client.socketFd.fd;
    client.pump(0);
    assert(!stream->closed && nghttp2_session_get_stream_user_data(client.session, id) == stream);
    ReapH2Streams(client, 0);
    assert(client.streams.count(id) == 1);
    blockedSendSocket = -1;
    client.pump(0);
    assert(stream->closed);
    ReapH2Streams(client, 0);
    assert(client.streams.empty());
}

void CheckResetDiscardsBlockedTarget(uint32_t error)
{
    auto state = std::make_shared<State>();
    state->config.authority = "localhost";
    SocketPair sockets, target;
    H2 client(state, nullptr, sockets.take(0));
    Server server(sockets.take(1));
    client.initialize();
    auto stream = std::make_unique<H2Stream>(target.take(0));
    stream->slot = std::make_unique<ConnectionSlot>(state);
    const int id = client.open("/reset", std::move(stream));
    client.pump(0);
    server.pump();
    server.respond(id, "200");
    std::string response = "buffered target bytes";
    server.body(id, response);
    client.pump(0);
    auto* pending = client.streams.at(id).get();
    assert(!pending->toSocket.empty() && state->connections == 1);
    blockedSendSocket = pending->socketFd.fd;
    server.reset(id, error);
    client.pump(0);
    blockedSendSocket = -1;
    if (error == NGHTTP2_NO_ERROR) {
        assert(pending->closed && !pending->toSocket.empty() && pending->socketFd.fd >= 0);
        failedSendSocket = pending->socketFd.fd;
        client.pump(0);
        failedSendSocket = -1;
    }
    assert(pending->closed && pending->toSocket.empty() && pending->socketFd.fd == -1);
    ReapH2Streams(client, 0);
    assert(client.streams.empty() && state->connections == 0);
}

void CheckBoundedClaimQueue()
{
    auto state = std::make_shared<State>();
    auto mapping = std::make_shared<Mapping>();
    // A cancelled mapping lets the worker finish without any network access.
    mapping->live = false;
    H2 client(state, mapping, -1);
    std::vector<PendingH2Dial> pending;
    client.streams.emplace(1, std::make_unique<H2Stream>());
    for (size_t index = 0; index < MAX_CONNECTIONS; ++index) {
        client.streams.emplace(static_cast<int>(index + 2), std::make_unique<H2Stream>());
    }
    assert(client.queueClaim("waiting"));
    assert(!StartNextH2Dial(client, pending));
    assert(client.claims.front() == "waiting" && pending.empty());
    client.streams.at(2)->closed = true;
    ReapH2Streams(client, 1);

    std::vector<std::unique_ptr<ConnectionSlot>> slots;
    for (size_t index = 0; index < MAX_CONNECTIONS; ++index) slots.push_back(std::make_unique<ConnectionSlot>(state));
    assert(!StartNextH2Dial(client, pending));
    assert(client.claims.front() == "waiting" && state->connections == MAX_CONNECTIONS);
    slots.pop_back();
    assert(StartNextH2Dial(client, pending));
    assert(client.claims.empty() && pending.size() == 1 && pending.front().token == "waiting");
    assert(state->connections == MAX_CONNECTIONS);
    const auto deadline = Clock::now() + std::chrono::seconds(2);
    while (!pending.front().dial->done.load(std::memory_order_acquire) && Clock::now() < deadline) {
        std::this_thread::sleep_for(std::chrono::milliseconds(1));
    }
    assert(pending.front().dial->done.load(std::memory_order_acquire));
    pending.clear();
    slots.clear();
    assert(state->connections == 0);

    for (size_t index = 0; index < MAX_CONNECTIONS; ++index) assert(client.queueClaim(std::to_string(index)));
    assert(!client.queueClaim("overflow"));
    assert(client.claimQueueOverflow && client.claims.size() == MAX_CONNECTIONS && client.claims.front() == "0");
}

void CheckSshClosing()
{
    auto* channel = reinterpret_cast<LIBSSH2_CHANNEL*>(0x1234);
    {
        SshRelay relay(-1, channel);
        relay.toChannel = {1, 2, 3, 4};
        relay.writePending = true;
        expectedBuffer = reinterpret_cast<const char*>(relay.toChannel.data());
        expectedLength = relay.toChannel.size();
        writeResults = {LIBSSH2_ERROR_EAGAIN, 4};
        freeResults = {LIBSSH2_ERROR_EAGAIN, 0};
        relay.beginClose();
        assert(relay.closeTick());
        assert(relay.toChannel.size() == 4 && relay.channel == channel && freeCalls == 0);
        assert(relay.closeTick());
        assert(relay.toChannel.empty() && relay.channel == channel && freeCalls == 1);
        assert(!relay.closeTick());
        assert(relay.channel == nullptr && freeCalls == 2 && writeCalls == 2 && readCalls == 2);
    }
    {
        SshRelay relay(-1, channel);
        relay.beginClose();
        relay.closeStarted = Clock::now() - std::chrono::seconds(11);
        bool timedOut = false;
        try { relay.closeTick(); }
        catch (const Failure&) { timedOut = true; }
        assert(timedOut && relay.channel == channel && freeCalls == 2);
    }
    assert(writeResults.empty() && freeResults.empty());
}
}

int main()
{
    CheckQueuedResetLifetime();
    CheckResetDiscardsBlockedTarget(NGHTTP2_CANCEL);
    CheckResetDiscardsBlockedTarget(NGHTTP2_NO_ERROR);
    CheckBoundedClaimQueue();
    CheckSshClosing();
    std::cout << "Native tunnel checks passed: queued RST lifetime, reset backpressure, bounded claim retry, SSH EAGAIN"
              << std::endl;
}
