package core

import (
	"context"
	"errors"
	"testing"

	"github.com/ThalesMMS/Go-PACS/internal/nodes"
	"github.com/ThalesMMS/Go-PACS/internal/retrieve"
)

func TestRetrieveRequiresRunningReceiverForMove(t *testing.T) {
	sess := openTestSession(t)

	_, err := sess.Retrieve(context.Background(), nodes.Node{
		Name:           "remote",
		AETitle:        "REMOTE",
		Host:           "127.0.0.1",
		Port:           11112,
		RetrieveMethod: nodes.RetrieveMethodMove,
	}, "STUDY", "1.2.3", "", "", nil)

	if !errors.Is(err, retrieve.ErrReceiverRequired) {
		t.Fatalf("Retrieve() error = %v, want ErrReceiverRequired", err)
	}
}
