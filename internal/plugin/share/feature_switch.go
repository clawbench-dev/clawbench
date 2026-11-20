package share

import (
	"fmt"
	"log/slog"

	"clawbench/internal/service"
)

// ShareDisableEffect revokes every issued share link when sharing is switched
// off (file, conversation and meeting links all at once).
//
// Links are server-issued capability URLs, so "off" has to mean they stop
// working everywhere — which one browser cannot do for another. Re-enabling does
// NOT restore them: that is the only honest reading of "revoke".
//
// Same warning semantics as ForgeDisableEffect: the switch already took effect.
func ShareDisableEffect() []string {
	res, err := service.RevokeAllShares()
	if err != nil {
		slog.Error("share: revoke all links after disable failed", "err", err)
		return []string{fmt.Sprintf("share disabled, but revoking existing links failed: %v", err)}
	}
	slog.Info(
		"share: master switch off, links revoked",
		"file", res.FileShares,
		"session", res.SessionShares,
		"meeting", res.MeetingShares,
	)
	return nil
}
