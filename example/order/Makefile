.PHONY: up ps down reset test test-integration tidy run

# 启动 MySQL + Redis 依赖
up:
	docker compose up -d

# 查看本地依赖健康状态
ps:
	docker compose ps

# 停止并删除容器（保留数据卷）
down:
	docker compose down

# 停止并删除容器与数据卷（彻底重置）
reset:
	docker compose down -v

# 纯单元测试（不依赖外部数据库）
test:
	go test ./...

# 集成测试（需先 make up，连接真实 MySQL + Redis）
test-integration:
	GOWORK=off go test -tags=integration ./... -count=1

tidy:
	GOWORK=off go mod tidy

run:
	go run ./cmd/orderserver
