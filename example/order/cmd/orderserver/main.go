package main

import (
	"log"
	"net/http"
	"time"

	"github.com/dirac-lee/domkit/config"
	"github.com/dirac-lee/domkit/example/order/internal/httpapi"
	"github.com/dirac-lee/domkit/example/order/internal/order"
)

func main() {
	// 配置源：演示环境用内存 Map；真实环境可替换为 env / 配置中心实现（同 Source 接口）。
	// 依赖本地 docker-compose 拉起的 MySQL(3306) 与 Redis(6379)。
	src := config.NewMapSource().
		Set("order.http-addr", ":8080").
		Set("order.default-page-size", "20").
		Set("order.mysql.dsn",
			"order:order@tcp(127.0.0.1:3306)/order?charset=utf8mb4&parseTime=True&loc=Local").
		Set("order.redis.addr", "127.0.0.1:6379")
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
	}

	log.Printf("order server listening on %s", server.Addr)
	log.Fatal(server.ListenAndServe())
}
