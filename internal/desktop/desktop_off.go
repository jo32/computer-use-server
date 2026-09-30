//go:build nogui

package desktop

import (
	"computer-use-server/internal/harness"
	"computer-use-server/internal/update"
	"fmt"
	"net/http"
)

const Available = false

func Run(http.Handler, *harness.Registry, *update.Manager, func(), string) error {
	return fmt.Errorf("this is a headless build; use the web subcommand")
}
