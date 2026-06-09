package workspacelock

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func testRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{
		Addr: "127.0.0.1:6379",
		DB:   15,
	})
	if err := client.Ping(context.Background()).Err(); err != nil {
		t.Skip("workspacelock tests require a local redis at 127.0.0.1:6379")
	}
	t.Cleanup(func() {
		_ = client.FlushDB(context.Background()).Err()
		_ = client.Close()
	})
	return client
}

func TestWorkspaceLocker_AcquireBusyAndRelease(t *testing.T) {
	client := testRedisClient(t)
	locker := NewWithConfig(client, 250*time.Millisecond, 80*time.Millisecond)

	_, err := client.Del(context.Background(), "workspace:project:proj-1").Result()
	require.NoError(t, err)

	leaseA, err := locker.Acquire(context.Background(), "proj-1", "agent-a")
	require.NoError(t, err)
	require.NotNil(t, leaseA)

	_, err = client.Get(context.Background(), "workspace:project:proj-1").Result()
	require.NoError(t, err)

	leaseB, err := locker.Acquire(context.Background(), "proj-1", "agent-b")
	require.Error(t, err)
	require.Nil(t, leaseB)
	require.Equal(t, "proj-1", asLockBusyErr(t, err).ProjectID)
	require.Equal(t, "agent-a", asLockBusyErr(t, err).Owner)

	require.NoError(t, leaseA.Release())
	time.Sleep(20 * time.Millisecond)

	leaseC, err := locker.Acquire(context.Background(), "proj-1", "agent-c")
	require.NoError(t, err)
	require.NotNil(t, leaseC)
	require.NoError(t, leaseC.Release())
}

func TestWorkspaceLocker_RefreshKeepsLeaseAlive(t *testing.T) {
	client := testRedisClient(t)
	locker := NewWithConfig(client, 120*time.Millisecond, 40*time.Millisecond)

	_, err := client.Del(context.Background(), "workspace:project:proj-2").Result()
	require.NoError(t, err)

	lease, err := locker.Acquire(context.Background(), "proj-2", "agent-refresh")
	require.NoError(t, err)
	require.NotNil(t, lease)

	time.Sleep(170 * time.Millisecond)
	ttl, err := client.PTTL(context.Background(), "workspace:project:proj-2").Result()
	require.NoError(t, err)
	require.Greater(t, int64(ttl/time.Millisecond), int64(20))

	require.NoError(t, lease.Release())
}

func TestWorkspaceLocker_NilClientReturnsNilLease(t *testing.T) {
	locker := New(nil)
	lease, err := locker.Acquire(context.Background(), "proj-nil", "owner")
	require.NoError(t, err)
	require.Nil(t, lease)
}

func asLockBusyErr(t *testing.T, err error) *LockBusyError {
	t.Helper()
	var lockErr *LockBusyError
	require.ErrorAs(t, err, &lockErr)
	return lockErr
}
