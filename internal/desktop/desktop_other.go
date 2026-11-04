//go:build !windows

package desktop

import "context"

func DataDirectory() string { return "data" }
func ShowError(error)       {}
func Run(ctx context.Context, _ string, _ context.CancelFunc, _ bool, ready func() error) error {
	if ready != nil {
		if err := ready(); err != nil {
			return err
		}
	}
	<-ctx.Done()
	return nil
}
