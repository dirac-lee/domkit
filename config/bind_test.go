package config

import (
	"errors"
	"testing"
	"time"
)

// testDBConfig 非嵌入嵌套 struct：键以 db. 为前缀。
type testDBConfig struct {
	Host string `config:"host"`
	Port int    `config:"port,default=8080"`
}

// testEmbedded 嵌入 struct：其字段提升，不额外加前缀。
type testEmbedded struct {
	Trace bool `config:"trace"`
}

// testOptions 综合演示 tag、kebab、默认值、嵌套、嵌入、忽略字段。
type testOptions struct {
	testEmbedded
	HTTPAddr string        `config:"http-addr,required"`
	PoolSize int           // 无 tag：自动 pool-size
	Timeout  time.Duration `config:"timeout,default=5s"`
	DB       testDBConfig  `config:"db"`
	Ignored  string        `config:"-"`
}

// TestBind_Success 验证 tag 取值、kebab 自动键、default 兜底与嵌套/嵌入。
func TestBind_Success(t *testing.T) {
	src := NewMapSourceFrom(map[string]string{
		"http-addr": ":8080",
		"pool-size": "16",
		"db.host":   "127.0.0.1",
	})

	got, err := Bind[testOptions](src, "")
	if err != nil {
		t.Fatalf("绑定失败: %v", err)
	}
	if got.HTTPAddr != ":8080" || got.PoolSize != 16 {
		t.Fatalf("标量字段异常: %+v", got)
	}
	if got.Timeout != 5*time.Second {
		t.Fatalf("default 超时异常: %v", got.Timeout)
	}
	if got.DB.Host != "127.0.0.1" || got.DB.Port != 8080 {
		t.Fatalf("嵌套字段异常: %+v", got.DB)
	}
	if got.Trace {
		t.Fatal("嵌入字段缺省应为 false")
	}
}

// TestBind_RequiredMissing 必填字段缺失应返回 BindingError。
func TestBind_RequiredMissing(t *testing.T) {
	src := NewMapSource()
	_, err := Bind[testOptions](src, "")

	var bindErr *BindingError
	if !errors.As(err, &bindErr) {
		t.Fatalf("期望 *BindingError，得到 %v", err)
	}
}

// TestBind_WithDefault 配置缺失字段取整体默认值实例的同名字段。
func TestBind_WithDefault(t *testing.T) {
	src := NewMapSourceFrom(map[string]string{"http-addr": ":9090"})
	defaults := testOptions{PoolSize: 10}

	got, err := Bind[testOptions](src, "", WithDefault(defaults))
	if err != nil {
		t.Fatalf("绑定失败: %v", err)
	}
	if got.PoolSize != 10 {
		t.Fatalf("应取整体默认值 PoolSize=10，得到 %d", got.PoolSize)
	}
}

// TestBind_Prefix 前缀应与字段键拼接。
func TestBind_Prefix(t *testing.T) {
	src := NewMapSourceFrom(map[string]string{"order.http-addr": ":7070"})

	got, err := Bind[testOptions](src, "order")
	if err != nil {
		t.Fatalf("绑定失败: %v", err)
	}
	if got.HTTPAddr != ":7070" {
		t.Fatalf("前缀拼接异常，得到 %q", got.HTTPAddr)
	}
}

// TestBind_NonStruct 非 struct 目标应直接报错。
func TestBind_NonStruct(t *testing.T) {
	if _, err := Bind[string](NewMapSource(), ""); err == nil {
		t.Fatal("非 struct 目标应报错")
	}
}
