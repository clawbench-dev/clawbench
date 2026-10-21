package service

import (
	"strings"
	"testing"
)

func TestBuildHostSystemPrompt(t *testing.T) {
	members := []HostMemberInfo{{Name: "A"}, {Name: "B"}, {Name: "C"}}
	p := BuildHostSystemPrompt("Host", members)
	if !strings.Contains(p, "<clawbench-mention targets=") {
		t.Fatal("must document the mention tag")
	}
	if !strings.Contains(p, "<clawbench-group-end/>") {
		t.Fatal("must document end signal")
	}
	if !strings.Contains(p, "结论") {
		t.Fatal("must ask for summary after end tag")
	}
	for _, m := range members {
		if !strings.Contains(p, m.Name) {
			t.Fatalf("member %q missing from prompt", m.Name)
		}
	}
	// The host must be told explicitly not to name itself (the self-route loop
	// defect: the host named itself and every "member turn" was a host turn).
	if !strings.Contains(p, "不要点名你自己") {
		t.Fatal("prompt must forbid the host from naming itself")
	}
}

// TestBuildHostSystemPrompt_Specialty: the host routes better when it knows what
// each member is good at (decision #39). A member with a specialty renders as
// "Name（specialty）"; one without renders as a bare name — no empty "（）".
func TestBuildHostSystemPrompt_Specialty(t *testing.T) {
	p := BuildHostSystemPrompt("Host", []HostMemberInfo{
		{Name: "Claude", Specialty: "代码编写与推理"},
		{Name: "Plain"},
	})
	if !strings.Contains(p, "Claude（代码编写与推理）") {
		t.Fatalf("specialty must render in parentheses after the name: %q", p)
	}
	if !strings.Contains(p, "Plain") {
		t.Fatalf("member without specialty must still be listed: %q", p)
	}
	if strings.Contains(p, "Plain（") {
		t.Fatalf("member without specialty must NOT render empty parentheses: %q", p)
	}
}

// TestBuildHostSystemPrompt_LeftMarked: a member that left keeps its slot in the
// prompt list (its past speech is still on the timeline) but must be visibly
// marked so the host does not route to it (decision #39).
func TestBuildHostSystemPrompt_LeftMarked(t *testing.T) {
	p := BuildHostSystemPrompt("Host", []HostMemberInfo{
		{Name: "Active", Specialty: "X"},
		{Name: "Gone", Left: true},
	})
	if !strings.Contains(p, "Gone（已离场）") {
		t.Fatalf("left member must be marked 已离场: %q", p)
	}
}

// TestBuildHostSystemPrompt_NoOtherMembers covers the "host-only group" shape:
// there is nobody to route to, so the prompt must tell the host to answer
// directly instead of naming a non-existent member.
func TestBuildHostSystemPrompt_NoOtherMembers(t *testing.T) {
	p := BuildHostSystemPrompt("Host", nil)
	if !strings.Contains(p, "没有其他成员") {
		t.Fatal("host-only prompt must state there are no other members")
	}
	if strings.Contains(p, "可选的成员名：（暂无其他成员）") == false {
		t.Fatal("host-only prompt must not present an empty member list as addressable")
	}
}

// TestBuildHostSystemPrompt_DelegatesWork pins the host's role boundary. The
// single "控场，而不是代替成员回答问题" sentence was too weak to be actionable:
// on task-shaped discussions the host did the work itself and only notified the
// members afterwards. The prompt must therefore (a) say what the host owns,
// (b) say the hands-on work goes to members and explicitly forbid doing it
// first, and (c) carve out the exceptions where the host MUST act (host-only
// group, user explicitly asking the host, e.g. /cb-*).
//
// The exception clause is not decoration: the host is a full member with tools
// and /cb-* is executed by the host (design decision #79), so a blanket ban
// would break both paths.
func TestBuildHostSystemPrompt_DelegatesWork(t *testing.T) {
	p := BuildHostSystemPrompt("Host", []HostMemberInfo{{Name: "A"}, {Name: "B"}})
	if !strings.Contains(p, "交给合适的成员去做") {
		t.Fatalf("host prompt must delegate hands-on work to members: %q", p)
	}
	// The actual failure shape was "finish it yourself, then tell them", so the
	// prompt has to forbid that ordering specifically.
	if !strings.Contains(p, "不要自己先做完再通知他们") {
		t.Fatalf("host prompt must forbid doing the work before delegating: %q", p)
	}
	if !strings.Contains(p, "拆解任务") {
		t.Fatalf("host prompt must state what the host DOES own: %q", p)
	}
	if !strings.Contains(p, "例外") || !strings.Contains(p, "/cb-*") {
		t.Fatalf("host prompt must carve out the exceptions (host-only group, /cb-*): %q", p)
	}

	// Host-only group: nobody to delegate to, so the host must still be told to
	// answer directly — the exception clause above must not contradict this.
	solo := BuildHostSystemPrompt("Host", nil)
	if !strings.Contains(solo, "直接回答用户即可") {
		t.Fatalf("host-only prompt must still tell the host to answer directly: %q", solo)
	}
	if !strings.Contains(solo, "没有其他成员") {
		t.Fatalf("host-only prompt must state there are no other members: %q", solo)
	}
}

// TestBuildHostSystemPrompt_ParallelDecisionRule: the host must be given an
// explicit rule for WHEN to pick parallel, not just the syntax. Without it the
// model defaults to sequential and rarely uses parallel (the reported defect:
// "agents don't tend to use it"). The rule must name the independent-opinion
// benefit AND the sequential default, so neither mode is a blind guess.
func TestBuildHostSystemPrompt_ParallelDecisionRule(t *testing.T) {
	p := BuildHostSystemPrompt("Host", []HostMemberInfo{{Name: "A"}, {Name: "B"}})
	if !strings.Contains(p, "mode=\"parallel\"") {
		t.Fatalf("host prompt must document the parallel attribute: %q", p)
	}
	// The choice rule itself, with both directions stated.
	if !strings.Contains(p, "独立意见") {
		t.Fatalf("host prompt must state the parallel benefit (independent opinions): %q", p)
	}
	if !strings.Contains(p, "互相补充") {
		t.Fatalf("host prompt must state the sequential case (mutual elaboration): %q", p)
	}
	// Sequential must be marked as the default so parallel reads as an active
	// choice rather than a rarely-considered option.
	if !strings.Contains(p, "默认") {
		t.Fatalf("host prompt must mark sequential as the default: %q", p)
	}
}

// TestBuildFreeMemberSystemPrompt_ParallelDecisionRule: same rule for a
// free-mode member, which is the other place agents @ multiple peers.
func TestBuildFreeMemberSystemPrompt_ParallelDecisionRule(t *testing.T) {
	p := BuildFreeMemberSystemPrompt([]HostMemberInfo{{Name: "A"}, {Name: "B"}}, "A")
	if !strings.Contains(p, "mode=\"parallel\"") {
		t.Fatalf("free prompt must document the parallel attribute: %q", p)
	}
	if !strings.Contains(p, "独立意见") {
		t.Fatalf("free prompt must state the parallel benefit: %q", p)
	}
	if !strings.Contains(p, "互相补充") {
		t.Fatalf("free prompt must state the sequential case: %q", p)
	}
	if !strings.Contains(p, "默认") {
		t.Fatalf("free prompt must mark sequential as the default: %q", p)
	}
}

func TestBuildHostSummaryPromptHasNoRoutingTag(t *testing.T) {
	p := BuildHostSummaryPrompt("Host", []HostMemberInfo{{Name: "A"}, {Name: "B"}})
	if strings.Contains(p, "clawbench-mention") || strings.Contains(p, "<clawbench-group-end/>") {
		t.Fatal("summary prompt must not ask for routing/end tags (would re-enter loop)")
	}
	if !strings.Contains(p, "结论") {
		t.Fatal("summary prompt must ask for a conclusion")
	}
}

// TestBuildHostSystemPrompt_DocumentsBcc: the host must be told how to send a
// private note, that targets are comma-separated and must be named this round,
// and that other members cannot see it.
func TestBuildHostSystemPrompt_DocumentsBcc(t *testing.T) {
	p := BuildHostSystemPrompt("Host", []HostMemberInfo{{Name: "A"}, {Name: "B"}})
	if !strings.Contains(p, "<clawbench-mention targets=") {
		t.Fatalf("prompt must document the bcc tag syntax: %q", p)
	}
	if !strings.Contains(p, "密送") {
		t.Fatalf("prompt must name the feature (密送): %q", p)
	}
	if !strings.Contains(p, "逗号") {
		t.Fatalf("prompt must explain comma-separated targets: %q", p)
	}
	if !strings.Contains(p, "看不到") {
		t.Fatalf("prompt must state other members cannot see the note: %q", p)
	}
}

// A host-only group has nobody to send a private note to, so the prompt must
// not document the private (密送) form (it would invite a note that goes
// nowhere). The plain mention form is still documented — it is how the host
// answers — so the check is for the `private` attribute, not the tag name.
func TestBuildHostSystemPrompt_NoMembersOmitsBcc(t *testing.T) {
	p := BuildHostSystemPrompt("Host", nil)
	if strings.Contains(p, "private") {
		t.Fatalf("host-only prompt must not document the private form: %q", p)
	}
}

// The summary prompt is a pure wrap-up: it must not invite any tag, bcc
// included (the orchestrator ignores tags in that turn).
func TestBuildHostSummaryPromptOmitsBcc(t *testing.T) {
	p := BuildHostSummaryPrompt("Host", []HostMemberInfo{{Name: "A"}})
	if strings.Contains(p, "clawbench-mention") {
		t.Fatalf("summary prompt must not document bcc: %q", p)
	}
}

// BuildMemberSystemPrompt: a group MEMBER must be told it is a participant, not
// the chair — without this, members imitated the host's routing tag and fought
// over the microphone (real incident: 3 members all opened with "🎙️ 主持人").
func TestBuildMemberSystemPrompt(t *testing.T) {
	p := BuildMemberSystemPrompt([]HostMemberInfo{{Name: "Host"}, {Name: "B"}, {Name: "C"}}, "B", "Host")
	if !strings.Contains(p, "参与者") {
		t.Fatalf("member prompt must state the participant role: %q", p)
	}
	if !strings.Contains(p, "不是主持人") {
		t.Fatalf("member prompt must say it is NOT the chair: %q", p)
	}
	// It must forbid emitting the host's protocol tags.
	if !strings.Contains(p, "clawbench-mention") || !strings.Contains(p, "不要") {
		t.Fatalf("member prompt must forbid emitting routing tags: %q", p)
	}
	// It must list who else is in the group (excluding self).
	if !strings.Contains(p, "Host") || !strings.Contains(p, "C") {
		t.Fatalf("member prompt must list other participants: %q", p)
	}
	if strings.Contains(p, "可选的成员名：") {
		t.Fatalf("member prompt must NOT carry the host's routing list: %q", p)
	}
}

// A member's private-note instruction must name the ACTUAL host.
//
// Mention targets resolve by display name, so a prompt that says
// targets="主持人名" hands the model a literal placeholder: it copies it, the
// target resolves to no member, and the note is silently dropped. The instruction
// therefore has to carry the host's group display name — which in production is
// the agent's own name ("Codebuddy"), never the word "Host".
func TestBuildMemberSystemPrompt_NamesTheHostForPrivateNotes(t *testing.T) {
	p := BuildMemberSystemPrompt(
		[]HostMemberInfo{{Name: "Codebuddy"}, {Name: "Alice"}}, "Alice", "Codebuddy",
	)
	if strings.Contains(p, "主持人名") {
		t.Fatalf("the placeholder must be replaced by the real host name: %q", p)
	}
	if !strings.Contains(p, `targets="Codebuddy" private`) {
		t.Fatalf("the note example must target the host by name: %q", p)
	}
}

// A free group has no host, so the private-channel paragraph is omitted rather
// than naming a non-existent chair. Free mode documents its own (unrestricted)
// private channel in BuildFreeMemberSystemPrompt.
func TestBuildMemberSystemPrompt_NoHostOmitsPrivateChannel(t *testing.T) {
	p := BuildMemberSystemPrompt([]HostMemberInfo{{Name: "A"}, {Name: "B"}}, "A", "")
	if strings.Contains(p, "只给主持人看的话") {
		t.Fatalf("without a host the host-only channel must be omitted: %q", p)
	}
	if strings.Contains(p, "主持人名") {
		t.Fatalf("no dangling placeholder: %q", p)
	}
}

// The host must see the user as a routable participant, and know that naming
// the user ends the round.
func TestBuildHostSystemPrompt_IncludesUser(t *testing.T) {
	p := BuildHostSystemPrompt("Host", []HostMemberInfo{{Name: "A"}, {Name: "B"}, {Name: groupUserTarget()}})
	if !strings.Contains(p, "可选的成员名：") || !strings.Contains(p, groupUserTarget()) {
		t.Fatalf("the user must be in the routable list: %q", p)
	}
	if !strings.Contains(p, "点到 User 后本轮结束") {
		t.Fatalf("the host must be told naming the user ends the round: %q", p)
	}
	// A note may target someone not named this round (deferred delivery).
	if strings.Contains(p, "必须是本轮") {
		t.Fatalf("the old 'target must be named this round' rule must be gone: %q", p)
	}
	if !strings.Contains(p, "下次被点名时送达") {
		t.Fatalf("the deferred-delivery rule must be documented: %q", p)
	}
}

// A member should know a human is in the room.
func TestBuildMemberSystemPrompt_ListsUser(t *testing.T) {
	p := BuildMemberSystemPrompt([]HostMemberInfo{{Name: "Host"}, {Name: "B"}}, "B", "Host")
	if !strings.Contains(p, groupUserTarget()) {
		t.Fatalf("a member must be told the user is present: %q", p)
	}
}

// A member must be told its OWN group display name. Without it, a duplicated
// instance falls back to the CLI's agent name ("Codebuddy") and every instance
// claims the same name — the real defect where four "Codebuddy" instances each
// misreported themselves.
func TestBuildMemberSystemPrompt_StatesOwnName(t *testing.T) {
	p := BuildMemberSystemPrompt(
		[]HostMemberInfo{{Name: "Codebuddy"}, {Name: "Codebuddy 2"}, {Name: "Codebuddy 3"}},
		"Codebuddy 3",
		"Codebuddy",
	)
	if !strings.Contains(p, "你的群内显示名是「Codebuddy 3」") {
		t.Fatalf("member prompt must state its own display name: %q", p)
	}
	// The others list must exclude self but include the sibling instances.
	if strings.Contains(p, "本群其他成员：Codebuddy 3") {
		t.Fatalf("others list must not include self: %q", p)
	}
	if !strings.Contains(p, "Codebuddy 2") {
		t.Fatalf("others list must include sibling instances: %q", p)
	}
}

// The HOST must also be told its own display name: when the host agent is
// duplicated, it must not lose its own identity.
func TestBuildHostSystemPrompt_StatesOwnName(t *testing.T) {
	p := BuildHostSystemPrompt("Codebuddy", []HostMemberInfo{{Name: "Codebuddy 2"}})
	if !strings.Contains(p, "你的群内显示名是「Codebuddy」") {
		t.Fatalf("host prompt must state its own display name: %q", p)
	}
	if !strings.Contains(p, "Codebuddy 2") {
		t.Fatalf("host prompt must still list the other instance: %q", p)
	}
}

// An empty self name must not produce a dangling identity sentence (the builder
// is also called from paths that may not have a name).
func TestSelfIdentityLine_EmptyIsOmitted(t *testing.T) {
	p := BuildMemberSystemPrompt([]HostMemberInfo{{Name: "A"}}, "", "A")
	if strings.Contains(p, "你的群内显示名是") {
		t.Fatalf("empty self name must omit the identity line: %q", p)
	}
}

// ── Auto-cruise prompts (design §15) ─────────────────────────────────────

// The host must learn that in cruise mode group-end no longer ends the
// discussion directly, and that goal-satisfied is the real end signal.
func TestBuildCruiseHostAddendum(t *testing.T) {
	p := BuildCruiseHostAddendum()
	if !strings.Contains(p, "自动巡航") {
		t.Fatalf("addendum must name the cruise mode: %q", p)
	}
	if !strings.Contains(p, "<clawbench-goal-satisfied/>") {
		t.Fatalf("addendum must document the goal-satisfied signal: %q", p)
	}
	if !strings.Contains(p, "<clawbench-group-end/>") || !strings.Contains(p, "不再直接结束") {
		t.Fatalf("addendum must state group-end no longer ends directly: %q", p)
	}
	// The HOST was the one cruising prompt that forgot to forbid asking the user
	// (free member / single chat / confirmation all say it). Cruise is
	// unattended, so a host question is a question nobody answers — the reported
	// "@ 了两拨人，反过来问我问题" timeline.
	if !strings.Contains(p, "不要向用户提问") {
		t.Fatalf("host addendum must forbid asking the user: %q", p)
	}
	if !strings.Contains(p, "<clawbench-ask-question>") {
		t.Fatalf("host addendum must name the ask-question card as forbidden: %q", p)
	}
}

// A free member must learn the completion declaration and must be told not to
// ask the user while cruising.
func TestBuildCruiseFreeMemberAddendum(t *testing.T) {
	p := BuildCruiseFreeMemberAddendum()
	if !strings.Contains(p, "<clawbench-goal-satisfied/>") {
		t.Fatalf("free addendum must document the goal-satisfied signal: %q", p)
	}
	if !strings.Contains(p, "不要向用户提问") {
		t.Fatalf("free addendum must forbid asking the user: %q", p)
	}
}

// The free addendum states the confirmation threshold, so its wording must match
// what countGoalSatisfied actually implements: votes*2 >= n, i.e. >= ceil(n/2).
// That is "at least half", NOT "over half" — with n=2 a single vote confirms
// (see TestCruiseFree_ConfirmationReachesHalf). The addendum originally said
// "过半" and d113c8629 corrected the i18n/docs but missed this builder, so the
// model kept being told the wrong rule. Pin both halves: the accurate phrase
// present AND the inaccurate one absent.
func TestBuildCruiseFreeMemberAddendum_ThresholdWording(t *testing.T) {
	p := BuildCruiseFreeMemberAddendum()
	if !strings.Contains(p, "至少半数") {
		t.Fatalf("free addendum must state the threshold as 至少半数 (>= ceil(n/2)): %q", p)
	}
	if strings.Contains(p, "过半") {
		t.Fatalf("free addendum must not say 过半 (over half) — that contradicts votes*2>=n: %q", p)
	}
}

// The confirmation prompt is hidden (not persisted) and forbids asking the
// user. The free variant forbids @-ing; the host variant invites routing when
// not done.
func TestBuildCruiseConfirmPrompt(t *testing.T) {
	free := BuildCruiseConfirmPrompt(true)
	if !strings.Contains(free, "<clawbench-goal-satisfied/>") {
		t.Fatalf("free confirm prompt must document the goal signal: %q", free)
	}
	if !strings.Contains(free, "不要向用户提问") {
		t.Fatalf("confirm prompt must forbid asking the user: %q", free)
	}
	if !strings.Contains(free, "不要 @ 任何人") {
		t.Fatalf("free confirm prompt must forbid @-ing: %q", free)
	}
	host := BuildCruiseConfirmPrompt(false)
	if !strings.Contains(host, "<clawbench-mention targets=") {
		t.Fatalf("host confirm prompt must invite routing when not done: %q", host)
	}
	if strings.Contains(host, "不要 @ 任何人") {
		t.Fatalf("host confirm prompt must NOT forbid @-ing: %q", host)
	}
	// The host's confirm branch must ask for the final conclusion IN THE SAME
	// reply: there is no separate summary turn after confirmation (design §15.7),
	// so a tag-only reply would leave the user without a wrap-up. But it must be
	// CONDITIONAL: a host that already wrote the conclusion in its previous reply
	// (the usual path — wrap-up, then <clawbench-group-end/>) only acknowledges
	// instead of repeating the whole block.
	if !strings.Contains(host, "最终结论") {
		t.Fatalf("host confirm prompt must ask for the conclusion in the same reply: %q", host)
	}
	if !strings.Contains(host, "上一条回复已经写出了完整的最终结论") {
		t.Fatalf("host confirm prompt must skip re-stating an already-written conclusion: %q", host)
	}
	if !strings.Contains(host, "不要重复展开") {
		t.Fatalf("host confirm prompt must forbid repeating the conclusion: %q", host)
	}
	// Both variants must tell the model to FINISH leftover work instead of
	// declaring the goal met: a confirmation round is the last chance to act.
	if !strings.Contains(host, "若仍存在遗留工作没有完成") {
		t.Fatalf("host confirm prompt must require finishing leftover work: %q", host)
	}
	if !strings.Contains(free, "若仍存在遗留工作没有完成") {
		t.Fatalf("free confirm prompt must require finishing leftover work: %q", free)
	}
	// The HOST must delegate the leftover work, not do it itself — the old
	// wording ("请先继续完成它们") directly contradicted the host's role boundary
	// and is the reason the host kept finishing tasks on its own. The free
	// variant is different: it has no host, so every member acts.
	if !strings.Contains(host, "点名合适的成员去完成") {
		t.Fatalf("host confirm prompt must delegate leftover work to members: %q", host)
	}
	if strings.Contains(host, "请先继续完成它们") {
		t.Fatalf("host confirm prompt must not tell the host to finish the work itself: %q", host)
	}
	// The free variant stays a bare vote — no conclusion (no host to write one).
	if strings.Contains(free, "最终结论") {
		t.Fatalf("free confirm prompt must stay a bare vote: %q", free)
	}
}

// The single-chat cruise addendum teaches the lone AI the completion signal, the
// no-asking rule, and that an unsolicited tag does not count.
func TestBuildCruiseSingleAddendum(t *testing.T) {
	p := BuildCruiseSingleAddendum()
	if !strings.Contains(p, "自动巡航") {
		t.Fatalf("addendum must name the cruise mode: %q", p)
	}
	if !strings.Contains(p, "<clawbench-goal-satisfied/>") {
		t.Fatalf("addendum must document the goal-satisfied signal: %q", p)
	}
	if !strings.Contains(p, "不要向用户提问") {
		t.Fatalf("addendum must forbid asking the user: %q", p)
	}
	// The strict rule: the AI must not emit the tag on its own.
	if !strings.Contains(p, "不要主动输出") {
		t.Fatalf("addendum must forbid unsolicited goal-satisfied: %q", p)
	}
}

// The single-chat confirmation prompt is hidden, forbids asking the user, and
// asks for the final conclusion in the same reply when satisfied. It must carry
// the SHARED confirmation marker so the e2e mock keys off it unchanged.
func TestBuildCruiseSingleConfirmPrompt(t *testing.T) {
	p := BuildCruiseSingleConfirmPrompt()
	if !strings.Contains(p, "[自动巡航·完成确认]") {
		t.Fatalf("confirm prompt must carry the shared marker (e2e mock keys off it): %q", p)
	}
	if !strings.Contains(p, "<clawbench-goal-satisfied/>") {
		t.Fatalf("confirm prompt must document the goal signal: %q", p)
	}
	if !strings.Contains(p, "不要向用户提问") {
		t.Fatalf("confirm prompt must forbid asking the user: %q", p)
	}
	if !strings.Contains(p, "最终结论") {
		t.Fatalf("confirm prompt must ask for the conclusion in the same reply: %q", p)
	}
	// ...unless the previous reply already carried it: then only acknowledge,
	// so the same long conclusion is not rendered twice on the timeline.
	if !strings.Contains(p, "上一条回复已经写出了完整的最终结论") {
		t.Fatalf("single-chat confirm prompt must skip re-stating an already-written conclusion: %q", p)
	}
	if !strings.Contains(p, "不要重复展开") {
		t.Fatalf("single-chat confirm prompt must forbid repeating the conclusion: %q", p)
	}
	if !strings.Contains(p, "若仍存在遗留工作没有完成") {
		t.Fatalf("single-chat confirm prompt must require finishing leftover work: %q", p)
	}
	// Single chat has no members to route to, so it must NOT invite mentions.
	if strings.Contains(p, "<clawbench-mention") {
		t.Fatalf("single-chat confirm prompt must not mention routing: %q", p)
	}
}
