package wechat

import (
	"testing"

	"clawbench/internal/push/common"
)

type fakeMessenger struct{ common.SessionMessenger }

func TestRegisterSettersStoreTheirArguments(t *testing.T) {
	prevChecker, prevMessenger := clientChecker, sessionMessenger
	t.Cleanup(func() { clientChecker, sessionMessenger = prevChecker, prevMessenger })

	checker := &fakeClientChecker{}
	RegisterClientChecker(checker)
	if clientChecker != checker {
		t.Fatal("RegisterClientChecker must store the checker")
	}

	msg := &fakeMessenger{}
	RegisterSessionMessenger(msg)
	if sessionMessenger != msg {
		t.Fatal("RegisterSessionMessenger must store the messenger")
	}
}

type fakeClientChecker struct{}

func (*fakeClientChecker) HasConnectedClients() bool { return false }
