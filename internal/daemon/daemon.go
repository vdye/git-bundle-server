package daemon

import (
	"context"
	"fmt"
	"runtime"

	"github.com/github/git-bundle-server/internal/cmd"
	"github.com/github/git-bundle-server/internal/common"
	"github.com/github/git-bundle-server/internal/log"
)

type DaemonConfig struct {
	Label       string
	Description string
	Program     string
	Arguments   []string
}

type DaemonStatus struct {
	message string
}

func (d DaemonStatus) Message() string {
	return d.message
}

func StatusUnknown(err error) DaemonStatus {
	return DaemonStatus{
		fmt.Sprintf("Could not determine status due to unknown error: %s", err.Error()),
	}
}

func StatusError(exitCode string) DaemonStatus {
	return DaemonStatus{
		fmt.Sprintf("Failed (code: %s)", exitCode),
	}
}

func StatusRunning() DaemonStatus { return DaemonStatus{"Running"} }

func StatusStopped() DaemonStatus { return DaemonStatus{"Stopped"} }

func StatusNotLoaded() DaemonStatus { return DaemonStatus{"Not loaded"} }

type DaemonProvider interface {
	Create(ctx context.Context, config *DaemonConfig, force bool) error

	Status(ctx context.Context, label string) DaemonStatus

	Start(ctx context.Context, label string) error

	Stop(ctx context.Context, label string) error

	Remove(ctx context.Context, label string) error
}

func NewDaemonProvider(
	l log.TraceLogger,
	u common.UserProvider,
	c cmd.CommandExecutor,
	fs common.FileSystem,
) (DaemonProvider, error) {
	switch thisOs := runtime.GOOS; thisOs {
	case "linux":
		// Use systemd/systemctl
		return NewSystemdProvider(l, u, c, fs), nil
	case "darwin":
		// Use launchd/launchctl
		return NewLaunchdProvider(l, u, c, fs), nil
	default:
		return nil, fmt.Errorf("cannot configure daemon handler for OS '%s'", thisOs)
	}
}
