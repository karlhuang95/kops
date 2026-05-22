package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	appserve "kops/internal/app/serve"
)

var servePort int
var serveCacheDir string

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the kops web dashboard",
	Long: `kops serve starts a real-time web dashboard for Kubernetes resource analysis.
It queries Prometheus on each page load and displays resource recommendations,
traffic efficiency rankings, and health status in a web interface.

Access the dashboard at http://localhost:<port>`,
	Run: func(cmd *cobra.Command, args []string) {
		cfgPath, _ := cmd.Flags().GetString("config")
		cfg, err := loadCommandConfig(cfgPath)
		if err != nil {
			printCommandError("failed to load config", err)
			return
		}

		srv := appserve.New(cfg, servePort, serveCacheDir)

		quit := make(chan os.Signal, 1)
		signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
		go func() {
			<-quit
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if err := srv.Shutdown(ctx); err != nil {
				fmt.Printf("server forced to shutdown: %v\n", err)
			}
		}()

		fmt.Printf("kops dashboard starting on http://localhost:%d\n", servePort)
		if err := srv.Start(); err != nil {
			printCommandError("server error", err)
		}
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
	serveCmd.Flags().IntVarP(&servePort, "port", "p", 8080, "web server listen port")
	serveCmd.Flags().StringVar(&serveCacheDir, "cache-dir", "./cache", "本地缓存目录")
}
