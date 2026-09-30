package readmodel

import "errors"

// 分页参数边界。页大小设上限，防止一次查询拉取过多数据。
const (
	minPageNumber = 1
	minPageSize   = 1
	maxPageSize   = 200
)

// PageRequest 分页请求（不可变值对象）。页码 pageNumber 从 1 开始。
type PageRequest struct {
	pageNumber int
	pageSize   int
}

// NewPageRequest 构造分页请求并做边界校验，非法入参直接返回错误（提前返回，不构造非法对象）。
func NewPageRequest(pageNumber, pageSize int) (PageRequest, error) {
	if pageNumber < minPageNumber {
		return PageRequest{}, errors.New("pageNumber 必须 >= 1")
	}
	if pageSize < minPageSize || pageSize > maxPageSize {
		return PageRequest{}, errors.New("pageSize 必须在 [1, 200] 区间")
	}
	return PageRequest{pageNumber: pageNumber, pageSize: pageSize}, nil
}

// PageNumber 返回当前页码（1-based）。
func (r PageRequest) PageNumber() int { return r.pageNumber }

// PageSize 返回每页大小。
func (r PageRequest) PageSize() int { return r.pageSize }

// Offset 返回偏移量，供 SQL/切片使用：(页码 - 1) * 页大小。
func (r PageRequest) Offset() int { return (r.pageNumber - 1) * r.pageSize }

// PageResult 分页结果（不可变值对象）。
type PageResult[T any] struct {
	data       []T // 当前页数据
	totalCount int64 // 满足条件的总条数
	request    PageRequest // 对应的分页请求
}

// NewPageResult 构造分页结果。
func NewPageResult[T any](data []T, totalCount int64, request PageRequest) PageResult[T] {
	return PageResult[T]{data: data, totalCount: totalCount, request: request}
}

// Data 返回当前页数据。
func (r PageResult[T]) Data() []T { return r.data }

// TotalCount 返回满足条件的总条数。
func (r PageResult[T]) TotalCount() int64 { return r.totalCount }

// Request 返回生成本结果的分页请求。
func (r PageResult[T]) Request() PageRequest { return r.request }
