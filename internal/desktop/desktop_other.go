//go:build !windows

package desktop

import "context"

func DataDirectory() string { return "data" }
func ShowError(error)       {}
func Run(ctx context.Context, _ string, _ context.CancelFunc) error {
	<-ctx.Done()
	return nil
}
