package wechat

import (
	"os"
	"testing"

	"clawbench/internal/model"
)

// TestMain pins the push locale to Chinese.
//
// Every push backend renders its bot replies through common.T(), which reads
// model.Language. Leaving it at the zero value would make assertions depend on
// whatever the surrounding test binary last wrote, so the locale is fixed here
// exactly like the DingTalk and Feishu packages do.
func TestMain(m *testing.M) {
	model.Language = model.DefaultLanguage
	os.Exit(m.Run())
}
