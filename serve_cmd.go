package main

import (
	"fmt"
	"net"
	"net/http"

	"github.com/spf13/cobra"

	"github.com/whoislikemiha/legwork/internal/config"
	serveui "github.com/whoislikemiha/legwork/internal/serve"
)

func serveCmd() *cobra.Command {
	var addr string
	var allowRemote bool
	c := &cobra.Command{
		Use:   "serve",
		Short: "Serve a local read-only browser dashboard",
		Long: `Serve a live read-only browser dashboard over the local legwork state directory.

The default binds to localhost only. Passing --addr with a non-loopback host is
rejected unless --allow-remote is also set; exposing the dashboard can reveal task,
event, path, and result data. The server has no mutation endpoints.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := serveui.ValidateAddr(addr, allowRemote); err != nil {
				return err
			}
			s, err := openStore()
			if err != nil {
				return err
			}
			health, err := config.LoadHealth()
			if err != nil {
				return err
			}
			ln, err := net.Listen("tcp", addr)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "legwork serve: %s\n", serveui.URL(ln.Addr()))
			return http.Serve(ln, serveui.NewHandlerWithOptions(s, serveui.Options{
				ContextThreshold: health.ContextThreshold,
				LocalOnly:        !allowRemote,
			}))
		},
	}
	c.Flags().StringVar(&addr, "addr", "127.0.0.1:0", "listen address (loopback only by default)")
	c.Flags().BoolVar(&allowRemote, "allow-remote", false, "allow --addr to bind a non-loopback host; read-only but exposes local job data")
	return c
}
