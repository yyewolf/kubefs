package cmd

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"bazil.org/fuse"
	"bazil.org/fuse/fs"
	"github.com/spf13/cobra"
	"github.com/yyewolf/kubefs/internal/kubefs"
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

		fuseConn, err := fuse.Mount(
			mountpoint,
			fuse.FSName("kubefs"),
			fuse.Subtype("kubefs"),
		)
		if err != nil {
			log.Fatal(err)
		}
		defer fuseConn.Close()

		var signalChan = make(chan os.Signal, 1)
		signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

		kubeFs := kubefs.KubeFS{
			Namespaces: make(map[string]*kubefs.Namespace),
		}
		kubeFs.EnsureNamespace("clusterwide", true)

		go func() {
			err = fs.Serve(fuseConn, kubeFs)
			if err != nil {
				log.Fatal(err)
			}
		}()

		kubefs.Inform(&kubeFs)

		<-signalChan
		err = fuse.Unmount(mountpoint)
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
