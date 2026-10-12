package main

import (
	"context"
	"flag"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"weavegrid/internal/workbench"
	"weavegrid/web"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8731", "监听地址")
	data := flag.String("data", "data", "数据目录")
	flag.Parse()
	if e := os.MkdirAll(*data, 0700); e != nil {
		log.Fatal(e)
	}
	// 先确认端口和数据目录独占，再恢复检查点，防止重复启动打断已有运行。
	listener, e := net.Listen("tcp", *addr)
	if e != nil {
		log.Fatal(e)
	}
	defer listener.Close()
	lock, e := os.OpenFile(filepath.Join(*data, "server.lock"), os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		log.Fatal(e)
	}
	defer lock.Close()
	if e = syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); e != nil {
		log.Fatal("数据目录已由工作台使用，请打开已运行的工作台")
	}
	defer syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	store, e := workbench.OpenStore(filepath.Join(*data, "novelflow.db"))
	if e != nil {
		log.Fatal(e)
	}
	defer store.Close()
	engine := workbench.NewEngine(store, &workbench.HTTPProvider{})
	defer engine.Close()
	api := workbench.NewServer(store, engine)
	mux := http.NewServeMux()
	mux.Handle("/api/", api.Handler())
	assets, e := fs.Sub(web.Files, "dist")
	if e != nil {
		log.Fatal(e)
	}
	mux.Handle("/", http.FileServerFS(assets))
	srv := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Printf("WeaveGrid · 经纬工作台：http://%s", *addr)
		if e := srv.Serve(listener); e != nil && e != http.ErrServerClosed {
			log.Fatal(e)
		}
	}()
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGTERM)
	<-signals
	engine.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}
