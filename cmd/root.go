package cmd

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/spf13/cobra"
	"github.com/yyewolf/kubefs/internal/kubefs"
	"k8s.io/utils/ptr"
)

var rootCmd = &cobra.Command{
	Use:   "kubefs [MOUNTPOINT]",
	Short: "A utility to mount Kubernetes resources as a filesystem",
	Long: `kubefs is a command-line tool that allows you to mount various
Kubernetes resources (like ConfigMaps, Secrets, etc.) as a filesystem on your local machine.
This can be useful for development and debugging purposes.`,
	Run: func(cmd *cobra.Command, args []string) {
		if len(args) != 1 {
			cmd.Usage()
			os.Exit(2)
		}
		mountpoint := args[0]

		var signalChan = make(chan os.Signal, 1)
		signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

		kubeFs := &kubefs.KubeFS{}
		server, err := fs.Mount(mountpoint, kubeFs, &fs.Options{
			AttrTimeout:  ptr.To(1 * time.Millisecond),
			EntryTimeout: ptr.To(1 * time.Millisecond),
		})
		if err != nil {
			log.Fatalf("Mount fail: %v\n", err)
		}

		kubeFs.AddNamespace(context.Background(), "clusterwide", true)
		kubefs.Inform(kubeFs)

		<-signalChan
		err = server.Unmount()
		if err != nil {
			log.Fatal(err)
		}
	},
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().BoolP("toggle", "t", false, "Help message for toggle")
}
