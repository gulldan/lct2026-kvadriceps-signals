package application

import (
	"context"
	"errors"
	"testing"

	"github.com/example/main-service/internal/domain"
	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
	"github.com/example/main-service/internal/platform/indexdoc"
)

type sourceAccessFake struct {
	allowed bool
	changes []indexdoc.AccessChange
	err     error
}

func (f *sourceAccessFake) ReadIndexes(_ context.Context, in indexdoc.Read) (indexdoc.States, error) {
	out := indexdoc.States{Epoch: 1}
	for _, id := range in.DocumentIDs {
		out.Documents = append(out.Documents, indexdoc.State{DocumentID: id, Allowed: f.allowed})
	}
	return out, f.err
}
func (f *sourceAccessFake) SetSourceAccess(_ context.Context, in indexdoc.AccessChange) error {
	f.changes = append(f.changes, in)
	return f.err
}

func TestSourceAccessChangeRequiresOwnerAndCanRestoreRevokedSource(t *testing.T) {
	docs, policy := newDocs(), &sourceAccessFake{}
	docs.docs["source"] = &commonv1.Document{Id: "source", OwnerId: "alice", SourceMetadata: "{}"}
	svc := &IngestionService{docs: docs, sourceIndexes: policy}
	err := svc.SetSourceAccess(t.Context(), indexdoc.AccessChange{DocumentID: "source", OwnerID: "admin"})
	if !errors.Is(err, ErrNotFound) || len(policy.changes) != 0 {
		t.Fatal("administrative fallback", err)
	}
	err = svc.SetSourceAccess(t.Context(), indexdoc.AccessChange{DocumentID: "source", OwnerID: "alice", Revoked: false})
	if err != nil || len(policy.changes) != 1 {
		t.Fatal("owner cannot restore revoked source", err)
	}
}

func TestSourceRevocationBlocksStoredAnswersAndDialogContext(t *testing.T) {
	chats, policy := newChats(), &sourceAccessFake{allowed: true}
	chats.chats["chat"] = &commonv1.Chat{Id: "chat", OwnerId: "alice"}
	chats.messages["chat"] = []*commonv1.Message{{Role: "assistant", Content: "restricted finding",
		Sources: []*commonv1.Source{{DocumentId: "source", SourceIndexSnapshot: "old-version"}}}}
	svc := NewChatService(chats, nil)
	svc.EnableSourceReads(policy, true)
	if _, err := svc.ListMessages(t.Context(), "alice", "chat"); err != nil {
		t.Fatal(err)
	}
	policy.allowed = false
	if messages, err := svc.ListMessages(t.Context(), "alice", "chat"); !errors.Is(err, ErrNotFound) || messages != nil {
		t.Fatal("stored answer disclosed after revoke", err)
	}
	_, err := svc.Ask(t.Context(), domain.AskCommand{OwnerID: "alice", ChatID: "chat", Content: "continue"})
	if !errors.Is(err, ErrNotFound) || chats.addMsgCalls != 1 {
		t.Fatal("revoked history used for generation", err)
	}
	policy.err = errors.New("DB unavailable")
	if _, err := svc.ListMessages(t.Context(), "alice", "chat"); err == nil {
		t.Fatal("access outage allowed history")
	}
}

func TestCorpusListsFilterSourcesBeforeCountsWithoutAdminFallback(t *testing.T) {
	docs := newDocs()
	docs.docs["legacy"] = &commonv1.Document{Id: "legacy", OwnerId: "bob"}
	docs.docs["private"] = &commonv1.Document{Id: "private", OwnerId: "bob", SourceMetadata: "{}"}
	docs.docs["public"] = &commonv1.Document{Id: "public", OwnerId: "bob", SourceMetadata: "{}"}
	svc := &IngestionService{docs: docs, sourceIndexes: evidencePolicy(func(in indexdoc.Read) indexdoc.States {
		return indexdoc.States{Documents: []indexdoc.State{
			{DocumentID: "private", Allowed: in.ReaderID == "bob"}, {DocumentID: "public", Allowed: true},
		}}
	})}
	for _, reader := range []string{"admin", "bob"} {
		visible, err := svc.ListAllDocuments(t.Context(), reader)
		want := 2
		if reader == "bob" {
			want = 3
		}
		if err != nil || len(visible) != want {
			t.Fatalf("%s can count a forbidden source: %d (%v)", reader, len(visible), err)
		}
	}
}
