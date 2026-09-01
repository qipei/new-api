package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 任务日志是管理员在看，按归属过滤会让视频预览对他们完全不可用；
// 但中转链路仍必须严格按归属取任务，放宽等于让用户拿别人的任务去改写或续费。
func TestGetTaskForViewingScopesByRole(t *testing.T) {
	require.NoError(t, DB.AutoMigrate(&Task{}))
	owner, other := 4001, 4002
	task := &Task{TaskID: "task-viewing-scope", UserId: owner, Status: TaskStatusSuccess}
	require.NoError(t, DB.Create(task).Error)
	t.Cleanup(func() { DB.Where("task_id = ?", task.TaskID).Delete(&Task{}) })

	t.Run("owner sees their own task", func(t *testing.T) {
		got, exists, err := GetTaskForViewing(owner, common.RoleCommonUser, task.TaskID)
		require.NoError(t, err)
		require.True(t, exists)
		assert.Equal(t, owner, got.UserId)
	})

	t.Run("another common user cannot see it", func(t *testing.T) {
		_, exists, err := GetTaskForViewing(other, common.RoleCommonUser, task.TaskID)
		require.NoError(t, err)
		assert.False(t, exists)
	})

	t.Run("admin sees tasks owned by anyone", func(t *testing.T) {
		got, exists, err := GetTaskForViewing(other, common.RoleAdminUser, task.TaskID)
		require.NoError(t, err)
		require.True(t, exists)
		assert.Equal(t, owner, got.UserId)
	})

	// 归属校验放宽只针对查看，中转仍走 GetByTaskId。
	t.Run("the relay lookup stays scoped to the owner", func(t *testing.T) {
		_, exists, err := GetByTaskId(other, task.TaskID)
		require.NoError(t, err)
		assert.False(t, exists)
	})
}
