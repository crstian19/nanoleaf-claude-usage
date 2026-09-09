package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/crstian19/nanoleaf-claude-usage/pkg/nanoleaf"
)

func newPairCmd() *cobra.Command {
	var (
		host    string
		timeout time.Duration
	)

	cmd := &cobra.Command{
		Use:   "pair",
		Short: "Get an API token from panels in pairing mode",
		Long: "Hold the controller's power button for 5-7 seconds until the LEDs\n" +
			"flash, then this returns a token.\n\n" +
			"Panels hold several tokens at once, so pairing again does not revoke\n" +
			"an existing integration's access.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()

			if _, err := fmt.Fprintf(cmd.ErrOrStderr(),
				"Hold the power button on %s for 5-7s until the LEDs flash...\n", host); err != nil {
				return err
			}

			deadline := time.Now().Add(timeout)
			for {
				token, err := nanoleaf.Pair(ctx, host)
				switch {
				case err == nil:
					// Printed on stdout alone so it can be
					// captured, and never logged elsewhere.
					// The prompt above went to stderr so that
					// `nanoclaude pair` can be piped directly.
					_, err := fmt.Fprintln(cmd.OutOrStdout(), token)
					return err
				case errors.Is(err, nanoleaf.ErrNotPairing):
					// Expected until the button is held.
				default:
					return err
				}

				if time.Now().After(deadline) {
					return fmt.Errorf("pair: %s never entered pairing mode within %s", host, timeout)
				}
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(2 * time.Second):
				}
			}
		},
	}

	cmd.Flags().StringVar(&host, "host", "", "panel controller address (required)")
	cmd.Flags().DurationVar(&timeout, "timeout", 3*time.Minute, "how long to wait for pairing mode")
	_ = cmd.MarkFlagRequired("host")
	return cmd
}
