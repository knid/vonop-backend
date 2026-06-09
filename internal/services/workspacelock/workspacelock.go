package workspacelock

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const (
	defaultTTL     = 60 * time.Second
	defaultRefresh = 20 * time.Second
)

const workspaceLockKeyPrefix = "workspace:project:"

var (
	refreshScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("PEXPIRE", KEYS[1], ARGV[2])
end
return 0
`)

	releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
end
return 0
`)
)

type LockBusyError struct {
	ProjectID string
	Owner     string
}

func (e *LockBusyError) Error() string {
	if e.Owner == "" {
		return fmt.Sprintf("workspace for project %s is already locked", e.ProjectID)
	}
	return fmt.Sprintf("workspace for project %s is locked by %s", e.ProjectID, e.Owner)
}

type Lease struct {
	client  *redis.Client
	key     string
	owner   string
	refresh time.Duration
	fired   bool
	stop    func()
	mu      sync.Mutex
	closed  chan struct{}
}

// RefreshLoop starts a background goroutine that refreshes the lock at the
// configured interval until ctx is canceled. Existing callers that rely on the
// legacy acquire-time goroutine can still treat this as a no-op.
func (l *Lease) RefreshLoop(ctx context.Context) {
	if l == nil || ctx == nil {
		return
	}
	go func() {
		ticker := time.NewTicker(l.refresh)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = l.touch()
			}
		}
	}()
}

func (l *Lease) Release() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	if l.fired {
		l.mu.Unlock()
		return nil
	}
	l.fired = true
	if l.stop != nil {
		l.stop()
	}
	close(l.closed)
	l.mu.Unlock()

	if l.client == nil {
		return nil
	}
	_, err := releaseScript.Run(context.Background(), l.client, []string{l.key}, l.owner).Result()
	if err != nil && err != redis.Nil {
		return err
	}
	return nil
}

type WorkspaceLocker struct {
	client  *redis.Client
	ttl     time.Duration
	refresh time.Duration
}

func New(client *redis.Client) *WorkspaceLocker {
	return &WorkspaceLocker{
		client:  client,
		ttl:     defaultTTL,
		refresh: defaultRefresh,
	}
}

func NewWithConfig(client *redis.Client, ttl, refresh time.Duration) *WorkspaceLocker {
	if ttl <= 0 {
		ttl = defaultTTL
	}
	if refresh <= 0 {
		refresh = defaultRefresh
	}
	if refresh >= ttl {
		refresh = ttl / 3
	}
	if refresh <= 0 {
		refresh = time.Second
	}
	return &WorkspaceLocker{client: client, ttl: ttl, refresh: refresh}
}

func (s *WorkspaceLocker) key(projectID string) string {
	return workspaceLockKeyPrefix + projectID
}

func (s *WorkspaceLocker) Acquire(ctx context.Context, projectID, owner string) (*Lease, error) {
	if s == nil || s.client == nil {
		return nil, nil
	}
	if projectID == "" {
		return nil, fmt.Errorf("workspace lock: project_id is required")
	}
	if owner == "" {
		return nil, fmt.Errorf("workspace lock: owner is required")
	}

	leaseKey := s.key(projectID)
	ok, err := s.client.SetNX(ctx, leaseKey, owner, s.ttl).Result()
	if err != nil {
		return nil, err
	}
	if !ok {
		held, getErr := s.client.Get(ctx, leaseKey).Result()
		if getErr != nil && getErr != redis.Nil {
			return nil, getErr
		}
		return nil, &LockBusyError{ProjectID: projectID, Owner: held}
	}

	leaseCtx, stop := context.WithCancel(context.Background())
	lease := &Lease{
		client:  s.client,
		key:     leaseKey,
		owner:   owner,
		refresh: s.refresh,
		stop:    stop,
		closed:  make(chan struct{}),
	}
	go lease.refreshLoop(leaseCtx)
	return lease, nil
}

func (l *Lease) refreshLoop(ctx context.Context) {
	ticker := time.NewTicker(l.refresh)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := l.touch(); err != nil {
				return
			}
		}
	}
}

func (l *Lease) touch() error {
	if l == nil {
		return nil
	}
	res, err := refreshScript.Run(context.Background(), l.client, []string{l.key}, l.owner, strconv.FormatInt(l.refresh.Milliseconds(), 10)).Result()
	if err != nil {
		return err
	}
	if resInt, ok := res.(int64); !ok || resInt != 1 {
		return redis.ErrClosed
	}
	return nil
}
