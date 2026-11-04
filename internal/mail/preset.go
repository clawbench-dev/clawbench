package mail

// Preset is a well-known provider's connection settings. The list lives here
// (not in the frontend) so the host/port facts have exactly one source; the
// UI fetches it from /api/mail/presets.
type Preset struct {
	ID string `json:"id"`
	// Label is a brand name — never translated.
	Label  string   `json:"label"`
	Domain []string `json:"domains,omitempty"`

	IMAPHost     string `json:"imapHost"`
	IMAPPort     int    `json:"imapPort"`
	IMAPSecurity string `json:"imapSecurity"`

	SMTPHost     string `json:"smtpHost"`
	SMTPPort     int    `json:"smtpPort"`
	SMTPSecurity string `json:"smtpSecurity"`

	// HintKey is an i18n key shown under the provider picker. Most Chinese
	// providers require an "authorization code" instead of the account
	// password, which is the single most common reason a first attempt fails.
	HintKey string `json:"hintKey,omitempty"`
}

// Presets is the provider list offered by the account form, in display order.
// "custom" is last and intentionally empty: it is the escape hatch for
// self-hosted servers.
var Presets = []Preset{
	{
		ID: "qq", Label: "QQ 邮箱", Domain: []string{"qq.com", "vip.qq.com", "foxmail.com"},
		IMAPHost: "imap.qq.com", IMAPPort: 993, IMAPSecurity: string(SecurityTLS),
		SMTPHost: "smtp.qq.com", SMTPPort: 465, SMTPSecurity: string(SecurityTLS),
		HintKey: "mail.hint.authCode",
	},
	{
		ID: "163", Label: "网易 163", Domain: []string{"163.com"},
		IMAPHost: "imap.163.com", IMAPPort: 993, IMAPSecurity: string(SecurityTLS),
		SMTPHost: "smtp.163.com", SMTPPort: 465, SMTPSecurity: string(SecurityTLS),
		HintKey: "mail.hint.authCode",
	},
	{
		ID: "126", Label: "网易 126", Domain: []string{"126.com"},
		IMAPHost: "imap.126.com", IMAPPort: 993, IMAPSecurity: string(SecurityTLS),
		SMTPHost: "smtp.126.com", SMTPPort: 465, SMTPSecurity: string(SecurityTLS),
		HintKey: "mail.hint.authCode",
	},
	{
		ID: "aliyun", Label: "阿里企业邮", Domain: []string{"aliyun.com"},
		IMAPHost: "imap.qiye.aliyun.com", IMAPPort: 993, IMAPSecurity: string(SecurityTLS),
		SMTPHost: "smtp.qiye.aliyun.com", SMTPPort: 465, SMTPSecurity: string(SecurityTLS),
	},
	{
		ID: "gmail", Label: "Gmail", Domain: []string{"gmail.com", "googlemail.com"},
		IMAPHost: "imap.gmail.com", IMAPPort: 993, IMAPSecurity: string(SecurityTLS),
		SMTPHost: "smtp.gmail.com", SMTPPort: 465, SMTPSecurity: string(SecurityTLS),
		HintKey: "mail.hint.appPassword",
	},
	{
		ID: "outlook", Label: "Outlook", Domain: []string{"outlook.com", "hotmail.com", "live.com"},
		IMAPHost: "outlook.office365.com", IMAPPort: 993, IMAPSecurity: string(SecurityTLS),
		SMTPHost: "smtp.office365.com", SMTPPort: 587, SMTPSecurity: string(SecurityStartTLS),
		HintKey: "mail.hint.appPassword",
	},
	{
		ID: "icloud", Label: "iCloud", Domain: []string{"icloud.com", "me.com"},
		IMAPHost: "imap.mail.me.com", IMAPPort: 993, IMAPSecurity: string(SecurityTLS),
		SMTPHost: "smtp.mail.me.com", SMTPPort: 587, SMTPSecurity: string(SecurityStartTLS),
		HintKey: "mail.hint.appPassword",
	},
	{
		ID: "custom", Label: "自定义", HintKey: "mail.hint.custom",
	},
}

// PresetByID returns the preset with the given id.
func PresetByID(id string) (Preset, bool) {
	for _, p := range Presets {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}
