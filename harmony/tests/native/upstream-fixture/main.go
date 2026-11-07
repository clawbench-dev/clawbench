//go:build ignore

// Run from the upstream ClawBench module. All listeners and targets are local;
// the public test password and cookie below must never be used on a real server.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"

	"clawbench/internal/handler"
	"clawbench/internal/model"
	"clawbench/internal/service"
	"golang.org/x/crypto/ssh"
)

const password = "fixture-tunnel-pass"
const page = `<!doctype html><html><meta name="viewport" content="width=device-width,initial-scale=1">
<body style="font:16px sans-serif;background:white;color:#111;padding:12px">
<h2>Tunnel fixture</h2><div id="buttons"></div><pre id="out" style="white-space:pre-wrap"></pre>
<script>
const n=window.ClawBenchHarmony; n.rendererReady(); n.dismissSplash();
function record(kind,value){out.textContent+=JSON.stringify({kind,value})+'\n';fetch('/result',{method:'POST',body:JSON.stringify({kind,value})});}
function act(name,run){let b=document.createElement('button');b.textContent=name;b.style='padding:12px;margin:4px;font-size:16px';
b.onclick=()=>Promise.resolve().then(run).catch(e=>record(name,{error:e.message}));buttons.append(b);}
act('H2',async()=>{await n.setTunnelTransportH2Enabled(true);record('h2','selected');});
act('SSH',async()=>{await n.setTunnelTransportH2Enabled(false);record('ssh','selected');});
act('Connect',async()=>record('connect',await n.reconnectTunnel()));
act('Status',async()=>record('status',{connected:await n.isTunnelConnected(),transport:await n.getActiveTunnelTransport(),error:await n.getTunnelError(),type:await n.getTunnelErrorType(),ports:await n.getForwardedPorts()}));
act('Forward',async()=>record('forward',await n.addForwardedPort(17582,20124,'127.0.0.1')));
act('Reverse',async()=>record('reverse',await n.addReverseForwardedPort(17682,17583,'127.0.0.1')));
act('Remove forward',async()=>record('remove-forward',await n.removeForwardedPort(17582)));
act('Remove reverse',async()=>record('remove-reverse',await n.removeReverseForwardedPort(17682)));
act('Browser forward',async()=>record('browser-forward',await n.addForwardedPort(17584,20121,'127.0.0.1')));
act('Open port',async()=>record('open-port',await n.openInBrowser(17584,'http','127.0.0.1','/browser-proof')));
act('Forget all',async()=>record('forget',await n.forgetForwardedPorts()));
act('Servers',()=>n.showServerDialog());
record('capabilities',n.getCapabilities());
</script></body></html>`

var results []json.RawMessage
var resultMu sync.Mutex

func relay(channel ssh.Channel, target net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(target, channel)
		if tcp, ok := target.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(channel, target)
		_ = channel.CloseWrite()
	}()
	wg.Wait()
	_ = channel.Close()
	_ = target.Close()
}

func serveSSH(signer ssh.Signer) {
	config := &ssh.ServerConfig{
		PasswordCallback: func(c ssh.ConnMetadata, value []byte) (*ssh.Permissions, error) {
			if c.User() == "clawbench" && string(value) == password {
				return nil, nil
			}
			return nil, errors.New("fixture password rejected")
		},
	}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:20122")
	if err != nil {
		log.Fatal(err)
	}
	for {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		go func() {
			server, channels, requests, err := ssh.NewServerConn(connection, config)
			if err != nil {
				_ = connection.Close()
				return
			}
			defer server.Close()
			listeners := make(map[uint32]net.Listener)
			var mu sync.Mutex
			defer func() {
				mu.Lock()
				defer mu.Unlock()
				for _, listener := range listeners {
					_ = listener.Close()
				}
			}()
			go func() {
				for request := range requests {
					switch request.Type {
					case "tcpip-forward", "cancel-tcpip-forward":
						var message struct {
							Address string
							Port    uint32
						}
						if ssh.Unmarshal(request.Payload, &message) != nil || message.Address != "127.0.0.1" ||
							message.Port < 17500 || message.Port > 17700 {
							_ = request.Reply(false, nil)
							continue
						}
						mu.Lock()
						if request.Type == "cancel-tcpip-forward" {
							if old := listeners[message.Port]; old != nil {
								_ = old.Close()
								delete(listeners, message.Port)
							}
							mu.Unlock()
							_ = request.Reply(true, nil)
							continue
						}
						listener, listenErr := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(int(message.Port)))
						if listenErr == nil {
							listeners[message.Port] = listener
						}
						mu.Unlock()
						_ = request.Reply(listenErr == nil, ssh.Marshal(struct{ Port uint32 }{message.Port}))
						if listenErr != nil {
							continue
						}
						go func(port uint32, listener net.Listener) {
							for {
								target, acceptErr := listener.Accept()
								if acceptErr != nil {
									return
								}
								payload := ssh.Marshal(struct {
									Address    string
									Port       uint32
									Origin     string
									OriginPort uint32
								}{"127.0.0.1", port, "127.0.0.1", 1})
								channel, requests, openErr := server.OpenChannel("forwarded-tcpip", payload)
								if openErr != nil {
									_ = target.Close()
									continue
								}
								go ssh.DiscardRequests(requests)
								go relay(channel, target)
							}
						}(message.Port, listener)
					case "keepalive@libssh2.org", "keepalive@openssh.com":
						_ = request.Reply(true, nil)
					default:
						_ = request.Reply(false, nil)
					}
				}
			}()
			for request := range channels {
				var target struct {
					Host       string
					Port       uint32
					Origin     string
					OriginPort uint32
				}
				if request.ChannelType() != "direct-tcpip" || ssh.Unmarshal(request.ExtraData(), &target) != nil ||
					target.Host != "127.0.0.1" || (target.Port != 20124 && target.Port != 20121) {
					_ = request.Reject(ssh.Prohibited, "fixture target refused")
					continue
				}
				connection, err := net.DialTimeout("tcp", net.JoinHostPort(target.Host, strconv.Itoa(int(target.Port))), time.Second)
				if err != nil {
					_ = request.Reject(ssh.ConnectionFailed, "fixture unreachable")
					continue
				}
				channel, requests, err := request.Accept()
				if err != nil {
					_ = connection.Close()
					continue
				}
				go ssh.DiscardRequests(requests)
				go relay(channel, connection)
			}
		}()
	}
}

func main() {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		log.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		log.Fatal(err)
	}
	hash := sha256.Sum256(signer.PublicKey().Marshal())
	fingerprint := hex.EncodeToString(hash[:])
	model.ServerPort = 20121
	// No application database is opened; the registry remains process-local.
	service.ProxyService = service.NewProxyRegistry(20121)
	service.ProxyService.SetAllowedPorts("17500-17700,20121,20124")
	go serveSSH(signer)
	go func() {
		listener, err := net.Listen("tcp", "127.0.0.1:20124")
		if err != nil {
			log.Fatal(err)
		}
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				_, _ = io.Copy(connection, connection)
				_, _ = connection.Write([]byte("|eof|"))
			}()
		}
	}()
	authorized := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("cb20121_clawbench_session")
			if err != nil || cookie.Value != "fixture-session" {
				http.Error(w, "fixture unauthorized", http.StatusUnauthorized)
				return
			}
			next(w, r)
		}
	}
	info := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"enabled": true, "port": 20122, "fingerprint": fingerprint})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"app":"clawbench","protocol":1,"version":"tunnel-fixture"}`)
	})
	mux.HandleFunc("/login", func(w http.ResponseWriter, r *http.Request) {
		http.SetCookie(w, &http.Cookie{Name: "cb20121_clawbench_session", Value: "fixture-session", Path: "/", HttpOnly: true})
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	})
	mux.HandleFunc("/api/ssh/info", info)
	mux.HandleFunc("/api/ssh/info/full", authorized(info))
	mux.HandleFunc("/api/tunnel/stream", authorized(handler.TunnelStream))
	mux.HandleFunc("/api/tunnel/control", authorized(handler.TunnelControl))
	mux.HandleFunc("/result", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 65536))
		if !json.Valid(body) {
			http.Error(w, "invalid result", http.StatusBadRequest)
			return
		}
		resultMu.Lock()
		if len(results) < 10000 {
			results = append(results, json.RawMessage(body))
		}
		resultMu.Unlock()
		_, _ = io.WriteString(w, "ok")
	})
	mux.HandleFunc("/results", func(w http.ResponseWriter, r *http.Request) {
		resultMu.Lock()
		defer resultMu.Unlock()
		_ = json.NewEncoder(w).Encode(results)
	})
	mux.HandleFunc("/browser-proof", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		resultMu.Lock()
		if len(results) < 10000 {
			results = append(results, json.RawMessage(`{"kind":"browser-proof","value":"native tunnel reached while external browser owns foreground"}`))
		}
		resultMu.Unlock()
		_, _ = io.WriteString(w, "<!doctype html><html><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><h1>Tunnel browser proof</h1><p>Reached the loopback fixture through the HarmonyOS native tunnel.</p></html>")
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, page)
	})
	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	server := &http.Server{Addr: "127.0.0.1:20121", Handler: mux, Protocols: protocols}
	fmt.Println("Native tunnel fixture: HTTP 20121, SSH 20122, echo 20124; loopback only")
	fmt.Println("SSH host key SHA-256 (hex):", fingerprint)
	log.Fatal(server.ListenAndServe())
}
