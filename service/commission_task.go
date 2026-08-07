package service

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
)

const commissionTickInterval = 30 * time.Second

var (
	commissionTaskOnce    sync.Once
	commissionTaskRunning atomic.Bool
)

// StartCommissionTask 启动推广返佣扫单任务(仅 master 节点)
func StartCommissionTask() {
	commissionTaskOnce.Do(func() {
		if !common.IsMasterNode {
			return
		}
		gopool.Go(func() {
			logger.LogInfo(context.Background(), fmt.Sprintf("commission task started: tick=%s", commissionTickInterval))
			model.EnsureCommissionScanIndex()
			ticker := time.NewTicker(commissionTickInterval)
			defer ticker.Stop()

			runCommissionScanOnce()
			for range ticker.C {
				runCommissionScanOnce()
			}
		})
	})
}

func runCommissionScanOnce() {
	if !commissionTaskRunning.CompareAndSwap(false, true) {
		return
	}
	defer commissionTaskRunning.Store(false)
	if _, err := model.ScanAndProcessCommissions(); err != nil {
		common.SysError("commission scan failed: " + err.Error())
	}
}
