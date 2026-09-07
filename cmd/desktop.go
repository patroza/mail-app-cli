package cmd

import (
	"context"
	"encoding/json"
	"github.com/intelligrit/mail-app-cli/pkg/desktop"
	"github.com/spf13/cobra"
	"time"
)

func init() {
	rootCmd.AddCommand(&cobra.Command{Use: "desktop", Short: "Private desktop client protocol (one JSON request on stdin)", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		ctx, cancel := context.WithTimeout(c.Context(), 90*time.Second)
		defer cancel()
		q, e := desktop.Decode(c.InOrStdin())
		var result map[string]any
		if e == nil {
			result, e = desktop.Execute(ctx, q)
		}
		if e != nil {
			result = map[string]any{"ok": false, "error": e.Error()}
		}
		return json.NewEncoder(c.OutOrStdout()).Encode(result)
	}})
}
