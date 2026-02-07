package cmd

import (
	"context"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/spf13/cobra"
	"github.com/yyewolf/kubefs/internal/kubefs"
	"k8s.io/utils/ptr"
)

var configPath string

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

		resolvedConfigPath, err := resolveConfigPath(configPath)
		if err != nil {
			log.Fatalf("Failed to resolve config path: %v", err)
		}

		config, err := kubefs.LoadConfig(resolvedConfigPath)
		if err != nil {
			log.Fatalf("Failed to load config: %v", err)
		}
		kubefs.SetLogLevel(config.LogLevel)

		var signalChan = make(chan os.Signal, 1)
		signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)

		kubeFs := kubefs.NewKubeFS(config)
		stopConfigWatch := make(chan struct{})
		startConfigWatcher(resolvedConfigPath, kubeFs, stopConfigWatch)
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
		close(stopConfigWatch)
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
	rootCmd.Flags().StringVar(&configPath, "config", "kubefs.yaml", "Path to config file (default: PWD/kubefs.yaml)")
}

func resolveConfigPath(path string) (string, error) {
	if filepath.IsAbs(path) {
		return path, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, path), nil
}

func startConfigWatcher(path string, kubeFs *kubefs.KubeFS, stopCh <-chan struct{}) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		log.Printf("Failed to start config watcher: %v", err)
		return
	}

	watchDir := filepath.Dir(path)
	if err := watcher.Add(watchDir); err != nil {
		log.Printf("Failed to watch config directory %s: %v", watchDir, err)
		_ = watcher.Close()
		return
	}

	go func() {
		defer func() {
			_ = watcher.Close()
		}()
		for {
			select {
			case <-stopCh:
				return
			case event, ok := <-watcher.Events:
				if !ok {
					return
				}
				if !shouldReloadConfig(event, path) {
					continue
				}
				config, err := kubefs.LoadConfig(path)
				if err != nil {
					log.Printf("Failed to reload config from %s: %v", path, err)
					continue
				}
				kubefs.SetLogLevel(config.LogLevel)
				kubeFs.SetConfig(config)
				log.Printf("Reloaded config from %s", path)
			case err, ok := <-watcher.Errors:
				if !ok {
					return
				}
				log.Printf("Config watcher error: %v", err)
			}
		}
	}()
}

func shouldReloadConfig(event fsnotify.Event, path string) bool {
	if filepath.Clean(event.Name) != filepath.Clean(path) {
		return false
	}
	return event.Has(fsnotify.Write) ||
		event.Has(fsnotify.Create) ||
		event.Has(fsnotify.Rename) ||
		event.Has(fsnotify.Remove)
}
