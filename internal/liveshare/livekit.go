package liveshare

// LiveKit control-plane client.
//
// This package deliberately stays on the standard library (see signal.go), and
// everything here fits: a LiveKit access token is a plain HS256 JWT, and its
// server API is Twirp over JSON — no protobuf, no gRPC, no SDK.
//
// What lives here is the CONTROL plane only: who may join, what they may
// publish, and removing them. Media never touches this code — the browsers talk
// to LiveKit directly.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// LiveKitConfig is the relay endpoint + credential, snapshotted onto a Room when
// it is created.
//
// A snapshot, not a live read: editing the global configuration must never
// change a meeting that is already running. Two participants on different
// endpoints would sit in different LiveKit rooms and never see each other while
// both screens say "joined".
type LiveKitConfig struct {
	// URL is what a browser connects to (ws:// or wss://).
	URL string
	// APIURL is what this server calls (http:// or https://), derived from URL.
	APIURL string
	// APIKey and APISecret are the same pair configured in LiveKit's `keys:`.
	APIKey    string
	APISecret string
}

// Configured reports whether this snapshot can actually be used.
func (c LiveKitConfig) Configured() bool {
	return c.URL != "" && c.APIURL != "" && c.APIKey != "" && c.APISecret != ""
}

// LiveKitGrant is the subset of a LiveKit video grant this feature needs.
type LiveKitGrant struct {
	Room           string `json:"room,omitempty"`
	RoomJoin       bool   `json:"roomJoin,omitempty"`
	RoomAdmin      bool   `json:"roomAdmin,omitempty"`
	CanPublish     bool   `json:"canPublish,omitempty"`
	CanSubscribe   bool   `json:"canSubscribe,omitempty"`
	CanPublishData bool   `json:"canPublishData,omitempty"`
}

// DefaultTokenTTL is how long a participant token stays valid.
//
// Long enough for a meeting that runs over, short enough that a leaked link
// stops working the same day. LiveKit keeps an already-connected participant
// alive past expiry; the TTL only bounds a NEW connection.
const DefaultTokenTTL = 12 * time.Hour

// JoinToken signs the credential a participant presents to LiveKit.
//
// `identity` must be unique per participant within the room: it is what the
// server API addresses when a permission changes or somebody is removed.
func (c LiveKitConfig) JoinToken(room, identity, name string, canPublish bool, ttl time.Duration) (string, error) {
	if !c.Configured() {
		return "", errors.New("liveshare: livekit is not configured")
	}
	if room == "" || identity == "" {
		return "", errors.New("liveshare: livekit token needs a room and an identity")
	}
	return c.sign(map[string]any{
		"sub":  identity,
		"name": name,
		"video": LiveKitGrant{
			Room:         room,
			RoomJoin:     true,
			CanSubscribe: true,
			CanPublish:   canPublish,
		},
	}, ttl)
}

// adminToken authorizes calls to LiveKit's server API.
func (c LiveKitConfig) adminToken(room string) (string, error) {
	return c.sign(map[string]any{
		"sub":   c.APIKey,
		"video": LiveKitGrant{Room: room, RoomAdmin: true},
	}, 5*time.Minute)
}

// sign builds an HS256 JWT.
//
// Hand-rolled on purpose: the whole format is three base64url segments and one
// HMAC, and pulling a JWT library in would be the only non-stdlib dependency
// this package has.
func (c LiveKitConfig) sign(claims map[string]any, ttl time.Duration) (string, error) {
	now := time.Now()
	body := map[string]any{
		"iss": c.APIKey,
		"nbf": now.Add(-10 * time.Second).Unix(),
		"exp": now.Add(ttl).Unix(),
	}
	for k, v := range claims {
		body[k] = v
	}
	header := map[string]any{"alg": "HS256", "typ": "JWT"}

	hb, err := json.Marshal(header)
	if err != nil {
		return "", err
	}
	cb, err := json.Marshal(body)
	if err != nil {
		return "", err
	}
	enc := base64.RawURLEncoding
	signingInput := enc.EncodeToString(hb) + "." + enc.EncodeToString(cb)

	mac := hmac.New(sha256.New, []byte(c.APISecret))
	_, _ = mac.Write([]byte(signingInput))
	return signingInput + "." + enc.EncodeToString(mac.Sum(nil)), nil
}

// ErrRelayUnreachable means the request never got an answer: wrong address, no
// service listening, firewall, DNS.
var ErrRelayUnreachable = errors.New("relay unreachable")

// ErrRelayRejected means LiveKit answered, but refused. The message carries
// what it said, which is the difference between "wrong key" and "wrong room".
var ErrRelayRejected = errors.New("relay rejected the request")

const relayTimeout = 8 * time.Second

// call posts one Twirp method and decodes the JSON reply into out.
func (c LiveKitConfig) call(ctx context.Context, method, room string, payload any, out any) error {
	if !c.Configured() {
		return errors.New("liveshare: livekit is not configured")
	}
	token, err := c.adminToken(room)
	if err != nil {
		return err
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, relayTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimSuffix(c.APIURL, "/")+"/twirp/livekit.RoomService/"+method, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRelayUnreachable, err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRelayUnreachable, err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		// Twirp puts a machine code and a human message in the body; both are
		// worth surfacing because "unauthorized" alone does not say WHICH of
		// the three fields is wrong.
		var twirpErr struct {
			Code string `json:"code"`
			Msg  string `json:"msg"`
		}
		_ = json.Unmarshal(raw, &twirpErr)
		detail := strings.TrimSpace(twirpErr.Msg)
		if detail == "" {
			detail = strings.TrimSpace(string(raw))
		}
		return fmt.Errorf("%w: HTTP %d %s %s", ErrRelayRejected, resp.StatusCode, twirpErr.Code, detail)
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("%w: %w", ErrRelayRejected, err)
	}
	return nil
}

// ListRooms is the connectivity + credential probe behind the "测试连接" button.
//
// It answers both questions in one call: reachability (did anything reply) and
// credentials (did it accept the token). A plain TCP probe would pass with a
// wrong secret, and the failure would only show up at the first meeting.
func (c LiveKitConfig) ListRooms(ctx context.Context) (int, error) {
	var out struct {
		Rooms []json.RawMessage `json:"rooms"`
	}
	if err := c.call(ctx, "ListRooms", "", struct{}{}, &out); err != nil {
		return 0, err
	}
	return len(out.Rooms), nil
}

// UpdateParticipantPermission grants or revokes the right to publish.
//
// The permission is REPLACED, not merged, so canSubscribe has to be restated —
// omitting it would silently turn the participant into a publisher that cannot
// hear anyone.
func (c LiveKitConfig) UpdateParticipantPermission(ctx context.Context, room, identity string, canPublish bool) error {
	return c.call(ctx, "UpdateParticipant", room, map[string]any{
		"room":     room,
		"identity": identity,
		"permission": map[string]any{
			"canSubscribe":   true,
			"canPublish":     canPublish,
			"canPublishData": false,
		},
	}, nil)
}

// RemoveParticipant hangs up one participant's connection.
func (c LiveKitConfig) RemoveParticipant(ctx context.Context, room, identity string) error {
	return c.call(ctx, "RemoveParticipant", room, map[string]any{
		"room":     room,
		"identity": identity,
	}, nil)
}
