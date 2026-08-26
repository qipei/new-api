package controller

import (
	"testing"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPaginateUpstreamCostAnomaliesFiltersAndKeepsLogOrder(t *testing.T) {
	logs := []*model.Log{{Id: 30}, {Id: 20}, {Id: 10}}
	costs := []service.UpstreamCostInfo{
		{LogId: 10, ExceedsPlatform: true},
		{LogId: 20, ExceedsPlatform: false},
		{LogId: 30, ExceedsPlatform: true},
	}

	page, total := paginateUpstreamCostAnomalies(logs, costs, 1, 1)

	assert.Equal(t, int64(2), total)
	require.Len(t, page, 1)
	assert.Equal(t, 10, page[0].Id)
}

func TestPaginateUpstreamCostAnomaliesReturnsEmptyPagePastTotal(t *testing.T) {
	logs := []*model.Log{{Id: 1}}
	costs := []service.UpstreamCostInfo{{LogId: 1, ExceedsPlatform: true}}

	page, total := paginateUpstreamCostAnomalies(logs, costs, 10, 100)

	assert.Equal(t, int64(1), total)
	assert.Empty(t, page)
}
