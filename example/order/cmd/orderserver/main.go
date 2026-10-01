package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/dirac-lee/domkit/config"
	"github.com/dirac-lee/domkit/example/order/internal/httpapi"
	"github.com/dirac-lee/domkit/example/order/internal/order"
)

func main() {
	// 配置源：环境变量优先，缺失时回退到本地 docker-compose 默认值，方便示例开箱即用。
	src := config.NewMapSource().
		Set("order.http-addr", envOr("ORDER_HTTP_ADDR", ":8080")).
		Set("order.default-page-size", envOr("ORDER_DEFAULT_PAGE_SIZE", "20")).
		Set("order.mysql.dsn",
			envOr("ORDER_MYSQL_DSN", "order:order@tcp(127.0.0.1:3306)/order?charset=utf8mb4&parseTime=True&loc=Local")).
		Set("order.redis.addr", envOr("ORDER_REDIS_ADDR", "127.0.0.1:6379"))
	cfg := config.NewContext(src)

	// 组合根：依据配置装配订单域全部进程级单例；失败直接终止（fail-fast）。
	app, err := order.NewApplication(cfg)
	if err != nil {
		log.Fatalf("bootstrap order application failed: %v", err)
	}

	// 仅负责启动：监听地址取自配置，路由与错误映射内聚在 httpapi。
	server := &http.Server{
		Addr:              app.Options.HTTPAddr,
		Handler:           httpapi.NewRouter(app, log.Default()),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	log.Printf("order server listening on %s", server.Addr)
	runErr := runServer(server)
	closeErr := app.Close()
	if runErr != nil {
		if closeErr != nil {
			log.Printf("close order application failed: %v", closeErr)
		}
		log.Printf("order server failed: %v", runErr)
		os.Exit(1)
	}
	if closeErr != nil {
		log.Printf("close order application failed: %v", closeErr)
		os.Exit(1)
	}
}

func runServer(server *http.Server) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		// ListenAndServe 在 Shutdown 后会返回 ErrServerClosed，这是正常退出路径。
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	case <-ctx.Done():
		log.Print("order server shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		if err := <-errCh; err != nil {
			return err
		}
		log.Print("order server stopped")
	}
	return nil
}

func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
