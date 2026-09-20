package docker

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeDocker records commands and replies from a script of canned output.
type fakeDocker struct {
	// replies maps a command prefix to its output.
	replies map[string]string
	// failures maps a command prefix to an error.
	failures map[string]error
	calls    []string
}

func newFake() *fakeDocker {
	return &fakeDocker{replies: map[string]string{}, failures: map[string]error{}}
}

func (f *fakeDocker) env() Env {
	return Env{
		Run: func(_ context.Context, args ...string) ([]byte, error) {
			command := strings.Join(args, " ")
			f.calls = append(f.calls, command)

			for prefix, err := range f.failures {
				if strings.HasPrefix(command, prefix) {
					return []byte("Error response from daemon: " + err.Error()), err
				}
			}
			for prefix, out := range f.replies {
				if strings.HasPrefix(command, prefix) {
					return []byte(out), nil
				}
			}
			return nil, nil
		},
	}
}

func (f *fakeDocker) ran(prefix string) bool {
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			return true
		}
	}
	return false
}

func TestInfo(t *testing.T) {
	fake := newFake()
	fake.replies["info"] = "27.0.1\noverlay2\n/var/lib/docker\n"

	info, err := New(fake.env()).Info(context.Background())
	if err != nil {
		t.Fatalf("Info() error = %v", err)
	}
	if info.ServerVersion != "27.0.1" || info.Driver != "overlay2" || info.RootDir != "/var/lib/docker" {
		t.Errorf("info = %+v", info)
	}
}

func TestInfoReportsAnUnreachableDaemon(t *testing.T) {
	fake := newFake()
	fake.failures["info"] = errors.New("exit status 1")

	_, err := New(fake.env()).Info(context.Background())
	if !errors.Is(err, ErrUnavailable) {
		t.Errorf("error = %v, want ErrUnavailable", err)
	}
}

func TestTakeSnapshot(t *testing.T) {
	fake := newFake()
	fake.replies["ps --all"] = "c2\nc1\n"
	fake.replies["volume ls"] = "v1\n"
	fake.replies["network ls"] = "n1\nn2\n"

	snap, err := New(fake.env()).Take(context.Background())
	if err != nil {
		t.Fatalf("Take() error = %v", err)
	}
	// Sorted, so a diff is stable regardless of what order docker listed.
	if strings.Join(snap.Containers, ",") != "c1,c2" {
		t.Errorf("Containers = %v", snap.Containers)
	}
	if len(snap.Volumes) != 1 || len(snap.Networks) != 2 {
		t.Errorf("snapshot = %+v", snap)
	}
	if snap.Total() != 5 {
		t.Errorf("Total() = %d, want 5", snap.Total())
	}
}

func TestAddedOnlyReportsWhatIsNew(t *testing.T) {
	before := Snapshot{
		Containers: []string{"existing"},
		Volumes:    []string{"cache"},
		Networks:   []string{"bridge", "host"},
	}
	after := Snapshot{
		Containers: []string{"existing", "job-container"},
		Volumes:    []string{"cache", "job-volume"},
		Networks:   []string{"bridge", "host", "job-network"},
	}

	added := Added(before, after)
	if strings.Join(added.Containers, ",") != "job-container" {
		t.Errorf("Containers = %v; a pre-existing container must not be touched", added.Containers)
	}
	if strings.Join(added.Volumes, ",") != "job-volume" {
		t.Errorf("Volumes = %v", added.Volumes)
	}
	if strings.Join(added.Networks, ",") != "job-network" {
		t.Errorf("Networks = %v", added.Networks)
	}
}

func TestAddedIsEmptyWhenNothingChanged(t *testing.T) {
	snap := Snapshot{Containers: []string{"a"}, Volumes: []string{"b"}}
	if !Added(snap, snap).Empty() {
		t.Error("Added() found changes between identical snapshots")
	}
	if !Added(snap, Snapshot{}).Empty() {
		t.Error("Added() reported removals as additions")
	}
}

func TestRemoveDeletesInDependencyOrder(t *testing.T) {
	fake := newFake()
	client := New(fake.env())

	result := client.Remove(context.Background(), Snapshot{
		Containers: []string{"c1"},
		Volumes:    []string{"v1"},
		Networks:   []string{"n1"},
	}, false)

	if result.Containers != 1 || result.Volumes != 1 || result.Networks != 1 {
		t.Errorf("result = %+v", result)
	}
	if result.Removed() != 3 {
		t.Errorf("Removed() = %d, want 3", result.Removed())
	}
	if len(result.Problems) != 0 {
		t.Errorf("problems = %v", result.Problems)
	}

	// Containers hold volumes and attach to networks, so they must go first
	// or the rest refuse.
	var order []string
	for _, c := range fake.calls {
		switch {
		case strings.HasPrefix(c, "rm --force"):
			order = append(order, "container")
		case strings.HasPrefix(c, "volume rm"):
			order = append(order, "volume")
		case strings.HasPrefix(c, "network rm"):
			order = append(order, "network")
		}
	}
	if strings.Join(order, ",") != "container,volume,network" {
		t.Errorf("removal order = %v", order)
	}
}

func TestRemoveReportsProblemsWithoutFailing(t *testing.T) {
	fake := newFake()
	fake.failures["volume rm --force v1"] = errors.New("volume is in use")

	result := New(fake.env()).Remove(context.Background(), Snapshot{
		Containers: []string{"c1"},
		Volumes:    []string{"v1", "v2"},
	}, false)

	// A stuck volume is worth telling an operator about, but the rest must
	// still be cleaned and the job must not be failed over it.
	if result.Containers != 1 || result.Volumes != 1 {
		t.Errorf("result = %+v", result)
	}
	if len(result.Problems) != 1 {
		t.Fatalf("problems = %v, want one", result.Problems)
	}
	if !strings.Contains(result.Problems[0], "volume v1") {
		t.Errorf("problem should name what stuck: %q", result.Problems[0])
	}
}

func TestPruneImagesOnlyWhenAsked(t *testing.T) {
	fake := newFake()
	New(fake.env()).Remove(context.Background(), Snapshot{}, false)
	if fake.ran("image prune") {
		t.Error("images were pruned without being asked")
	}

	fake = newFake()
	result := New(fake.env()).Remove(context.Background(), Snapshot{}, true)
	if !fake.ran("image prune --force") {
		t.Errorf("images were not pruned: %v", fake.calls)
	}
	if result.Images != 1 {
		t.Errorf("Images = %d", result.Images)
	}
}

func TestRemoveWithNothingToDoRunsNothing(t *testing.T) {
	fake := newFake()
	result := New(fake.env()).Remove(context.Background(), Snapshot{}, false)
	if len(fake.calls) != 0 {
		t.Errorf("ran %v for an empty snapshot", fake.calls)
	}
	if result.Removed() != 0 {
		t.Errorf("Removed() = %d", result.Removed())
	}
}

func TestSnapshotErrorsPropagate(t *testing.T) {
	fake := newFake()
	fake.failures["volume ls"] = errors.New("exit status 1")

	if _, err := New(fake.env()).Take(context.Background()); err == nil {
		t.Error("Take() hid a failure listing volumes")
	}
}
