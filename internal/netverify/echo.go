package netverify

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"

	"github.com/ThalesMMS/dicom-go/net/dimse"
	"github.com/ThalesMMS/dicom-go/net/ul"
	"github.com/ThalesMMS/Go-PACS/internal/nettimeout"
	"github.com/ThalesMMS/Go-PACS/internal/nodes"
)

const (
	DefaultCallingAETitle = "GOPACS"
	DefaultDialTimeout    = 10 * time.Second
	DefaultReleaseTimeout = 5 * time.Second
)

type EchoResult struct {
	NodeName  string
	Address   string
	Status    uint16
	Duration  time.Duration
	StartedAt time.Time
}

func Echo(ctx context.Context, node nodes.Node, callingAETitle string) (EchoResult, error) {
	if callingAETitle == "" {
		callingAETitle = DefaultCallingAETitle
	}
	callingAETitle = nodes.NormalizeAETitle(callingAETitle)
	if err := nodes.ValidateAETitle(callingAETitle); err != nil {
		return EchoResult{}, fmt.Errorf("calling AE title: %w", err)
	}
	address := net.JoinHostPort(node.Host, strconv.Itoa(int(node.Port)))
	started := time.Now()

	dialCtx, cancelDial := nettimeout.WithDefault(ctx, DefaultDialTimeout)
	defer cancelDial()
	assoc, err := ul.DialContext(dialCtx, address, ul.DialOptions{
		CalledAETitle:  node.AETitle,
		CallingAETitle: callingAETitle,
		Contexts: []ul.PresentationContext{{
			AbstractSyntaxUID:  dimse.VerificationSOPClassUID,
			TransferSyntaxUIDs: []string{ul.ImplicitVRLittleEndian},
		}},
	})
	if err != nil {
		return EchoResult{}, fmt.Errorf("associate with %s (%s): %w", node.Name, address, err)
	}
	released := false
	defer func() {
		if !released {
			_ = assoc.Close()
		}
	}()

	pc, ok := dimse.AcceptedVerificationContext(assoc)
	if !ok {
		return EchoResult{}, fmt.Errorf("verification presentation context was not accepted by %s", node.Name)
	}
	response, err := dimse.SendCEcho(assoc, pc.ID, 1)
	if err != nil {
		return EchoResult{}, fmt.Errorf("send C-ECHO to %s (%s): %w", node.Name, address, err)
	}

	releaseCtx, cancelRelease := context.WithTimeout(ctx, DefaultReleaseTimeout)
	defer cancelRelease()
	if err := assoc.Release(releaseCtx); err != nil {
		return EchoResult{}, fmt.Errorf("release association with %s (%s): %w", node.Name, address, err)
	}
	released = true

	return EchoResult{
		NodeName:  node.Name,
		Address:   address,
		Status:    response.Status,
		Duration:  time.Since(started),
		StartedAt: started.UTC(),
	}, nil
}
