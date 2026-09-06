package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/intelligrit/mail-app-cli/pkg/categories"
	"github.com/spf13/cobra"
	"strconv"
	"time"
)

func init() {
	group := &cobra.Command{Use: "categories", Short: "Read category membership and change sender categorization (macOS 15.4+)"}
	group.Long = "Read Primary through Apple's native category resolver and read-only Mail database. UI-based get/sender/set and --backend ax require Mail's graphical session and Accessibility/Automation. Setting a category affects every message from the sender, including future mail."
	group.AddCommand(&cobra.Command{Use: "list", Args: cobra.NoArgs, RunE: func(c *cobra.Command, _ []string) error {
		return json.NewEncoder(c.OutOrStdout()).Encode(categories.Names)
	}})
	for _, spec := range []struct {
		use, short, operation string
		count                 int
	}{
		{"get MESSAGE_ID", "Read exact category view membership of an already-read message", "membership", 1},
		{"sender MESSAGE_ID", "Read the sender's automatic or explicit category rule", "inspect", 1},
		{"set MESSAGE_ID CATEGORY", "Set sender rule: Primary, Transactions, Updates, Promotions or Automatically", "apply", 2},
	} {
		s := spec
		group.AddCommand(&cobra.Command{Use: s.use, Short: s.short, Args: cobra.ExactArgs(s.count), RunE: func(c *cobra.Command, args []string) error {
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return err
			}
			destination := ""
			if len(args) == 2 {
				destination = args[1]
			}
			ctx, cancel := context.WithTimeout(c.Context(), 60*time.Second)
			defer cancel()
			r, err := categories.Execute(ctx, s.operation, id, destination)
			if err != nil {
				return err
			}
			return json.NewEncoder(c.OutOrStdout()).Encode(r)
		}})
	}
	primary := &cobra.Command{Use: "primary", Short: "Read native Primary categories without a Mail window or marking messages read", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			limit, _ := c.Flags().GetInt("limit")
			ctx, cancel := context.WithTimeout(c.Context(), 75*time.Second)
			defer cancel()
			backend, _ := c.Flags().GetString("backend")
			var feed json.RawMessage
			var err error
			switch backend {
			case "native":
				feed, err = categories.NativePrimary(ctx, limit)
			case "ax":
				feed, err = categories.Primary(ctx, limit)
			default:
				return fmt.Errorf("backend must be native or ax")
			}
			if err != nil {
				return err
			}
			_, err = c.OutOrStdout().Write(append(feed, '\n'))
			return err
		}}
	primary.Flags().Int("limit", 100, "Maximum Primary messages (1–1000)")
	primary.Flags().String("backend", "native", "Category source: native or ax (UI diagnostic fallback)")
	group.AddCommand(primary)
	native := &cobra.Command{Use: "native", Short: "Experimental read-only native category inspection; no Mail window required", Args: cobra.NoArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			limit, _ := c.Flags().GetInt("limit")
			ctx, cancel := context.WithTimeout(c.Context(), 60*time.Second)
			defer cancel()
			result, err := categories.NativeInspect(ctx, limit)
			if err != nil {
				return err
			}
			_, err = c.OutOrStdout().Write(append(result, '\n'))
			return err
		}}
	native.Flags().Int("limit", 100, "Recent inbox messages to inspect (1–1000)")
	group.AddCommand(native)
	rootCmd.AddCommand(group)
}
