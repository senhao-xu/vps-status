package control

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/senhao-xu/vps-status/internal/security"
)

func probeAssignmentIDs(ids ...string) *[]string { return &ids }

func TestProbeAssignmentPatchMergesEnrollmentAndExplicitChoices(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	for _, id := range []string{"keep", "remove", "add"} {
		seedProbeNode(t, store, id, now)
	}
	task := autoJoinTask("task")
	if _, err := store.CreateProbeTask(t.Context(), task, []string{"keep", "remove"}, now); err != nil {
		t.Fatal(err)
	}
	// A later enrollment updates the committed set after an editor cached its task.
	token := EnrollmentToken{ID: "token", Digest: security.DigestEnrollmentToken("token"), NodeName: "automatic", CreatedAt: now, ExpiresAt: now.Add(time.Hour)}
	if err := store.CreateEnrollmentToken(t.Context(), token); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ConsumeEnrollment(t.Context(), token.Digest, "automatic", "credential", security.DigestNodeCredential("credential"), now); err != nil {
		t.Fatal(err)
	}
	before, err := store.ProbeSnapshot(t.Context(), "automatic")
	if err != nil {
		t.Fatal(err)
	}
	name := "renamed"
	patch := ProbeTaskPatch{Name: &name, NodeIDsAdd: probeAssignmentIDs("add"), NodeIDsRemove: probeAssignmentIDs("remove", "already-removed")}
	saved, old, err := store.PatchProbeTask(t.Context(), task.ID, patch, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(old, []string{"automatic", "keep", "remove"}) || !reflect.DeepEqual(saved.NodeIDs, []string{"add", "automatic", "keep"}) || saved.Name != name {
		t.Fatalf("merged task=%+v old=%v", saved, old)
	}
	after, err := store.ProbeSnapshot(t.Context(), "automatic")
	if err != nil || after.Version != before.Version+1 || len(after.Tasks) != 1 {
		t.Fatalf("snapshot=%+v error=%v", after, err)
	}
	// Replaying explicit choices and empty deltas cannot remove untouched nodes.
	for _, next := range []ProbeTaskPatch{patch, {NodeIDsAdd: probeAssignmentIDs(), NodeIDsRemove: probeAssignmentIDs()}, {}} {
		got, _, err := store.PatchProbeTask(t.Context(), task.ID, next, now.Add(2*time.Minute))
		if err != nil || !reflect.DeepEqual(got.NodeIDs, saved.NodeIDs) {
			t.Fatalf("idempotent patch=%+v error=%v", got, err)
		}
	}
	// Legacy explicit replacement remains a deliberate complete replacement.
	got, _, err := store.PatchProbeTask(t.Context(), task.ID, ProbeTaskPatch{NodeIDs: probeAssignmentIDs()}, now)
	if err != nil || len(got.NodeIDs) != 0 {
		t.Fatalf("legacy clear=%+v error=%v", got, err)
	}
}

func TestProbeAssignmentPatchRejectsMalformedChangesWithoutMutation(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	seedProbeNode(t, store, "node", now)
	task := autoJoinTask("task")
	if _, err := store.CreateProbeTask(t.Context(), task, []string{"node"}, now); err != nil {
		t.Fatal(err)
	}
	original, err := store.ProbeTaskByID(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.ProbeSnapshot(t.Context(), "node")
	if err != nil {
		t.Fatal(err)
	}
	tooMany := make([]string, 10001)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("node-%d", i)
	}
	for _, tc := range []struct {
		name  string
		patch ProbeTaskPatch
	}{
		{"replacement with addition", ProbeTaskPatch{NodeIDs: probeAssignmentIDs(), NodeIDsAdd: probeAssignmentIDs()}},
		{"replacement with removal", ProbeTaskPatch{NodeIDs: probeAssignmentIDs("node"), NodeIDsRemove: probeAssignmentIDs()}},
		{"duplicate additions", ProbeTaskPatch{NodeIDsAdd: probeAssignmentIDs("node", "node")}},
		{"duplicate removals", ProbeTaskPatch{NodeIDsRemove: probeAssignmentIDs("node", "node")}},
		{"overlapping changes", ProbeTaskPatch{NodeIDsAdd: probeAssignmentIDs("node"), NodeIDsRemove: probeAssignmentIDs("node")}},
		{"empty ID", ProbeTaskPatch{NodeIDsAdd: probeAssignmentIDs("")}},
		{"overlong ID", ProbeTaskPatch{NodeIDsRemove: probeAssignmentIDs(strings.Repeat("x", 129))}},
		{"control character", ProbeTaskPatch{NodeIDsRemove: probeAssignmentIDs("node\n")}},
		{"invalid UTF-8", ProbeTaskPatch{NodeIDsAdd: probeAssignmentIDs(string([]byte{0xff}))}},
		{"too many changes", ProbeTaskPatch{NodeIDsAdd: &tooMany}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := "must roll back"
			tc.patch.Name = &name
			if _, _, err := store.PatchProbeTask(t.Context(), task.ID, tc.patch, now.Add(time.Minute)); !errors.Is(err, ErrInvalidProbeConfiguration) {
				t.Fatalf("invalid patch error=%v", err)
			}
			got, err := store.ProbeTaskByID(t.Context(), task.ID)
			if err != nil || !reflect.DeepEqual(got, original) {
				t.Fatalf("invalid patch changed task=%+v error=%v", got, err)
			}
			after, err := store.ProbeSnapshot(t.Context(), "node")
			if err != nil || after.Version != before.Version {
				t.Fatalf("invalid patch changed version=%+v error=%v", after, err)
			}
		})
	}
}

func TestProbeAssignmentPatchQuotaAndMissingNodeRollBackAllChanges(t *testing.T) {
	store := openTestStore(t)
	now := time.Unix(1_700_000_000, 0).UTC()
	seedProbeNode(t, store, "existing", now)
	seedProbeNode(t, store, "full", now)
	task := autoJoinTask("target")
	if _, err := store.CreateProbeTask(t.Context(), task, []string{"existing"}, now); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 64; i++ {
		filler := autoJoinTask(fmt.Sprintf("filler-%d", i))
		filler.AutoJoin = false
		if _, err := store.CreateProbeTask(t.Context(), filler, []string{"full"}, now); err != nil {
			t.Fatal(err)
		}
	}
	original, err := store.ProbeTaskByID(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	before, err := store.ProbeSnapshot(t.Context(), "existing")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		add string
		err error
	}{{"full", ErrProbeTaskLimit}, {"missing", ErrNotFound}} {
		name, autoJoin := "must roll back", false
		patch := ProbeTaskPatch{Name: &name, AutoJoin: &autoJoin, NodeIDsAdd: probeAssignmentIDs(tc.add), NodeIDsRemove: probeAssignmentIDs("existing")}
		if _, _, err := store.PatchProbeTask(t.Context(), task.ID, patch, now.Add(time.Minute)); !errors.Is(err, tc.err) {
			t.Fatalf("add %s: error=%v want=%v", tc.add, err, tc.err)
		}
		got, err := store.ProbeTaskByID(t.Context(), task.ID)
		if err != nil || !reflect.DeepEqual(got, original) {
			t.Fatalf("failed patch changed task=%+v error=%v", got, err)
		}
		after, err := store.ProbeSnapshot(t.Context(), "existing")
		if err != nil || after.Version != before.Version {
			t.Fatalf("failed patch changed snapshot=%+v error=%v", after, err)
		}
	}
}
