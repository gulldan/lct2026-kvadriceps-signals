package application

import (
	"context"
	"encoding/json"
	"fmt"
	dbv1 "github.com/example/main-service/internal/platform/genproto/db/v1"
	"regexp"
	"slices"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/example/main-service/internal/domain"
	"github.com/example/main-service/internal/platform/logger"

	commonv1 "github.com/example/main-service/internal/platform/genproto/common/v1"
)

// Чат берёт фрагментов с запасом: llm-service переранжирует, и глубже выборка — точнее контекст.
// adaptiveTopK расширяет выборку до chatTopKMax на коротких общих вопросах.
const (
	defaultTopK int32 = 8
	chatTopKMax int32 = 12

	historyQuestionsMax = 3
	historyQuestionClip = 240
	historyAnswerClip   = 400
)

var citationLabelRe = regexp.MustCompile(`\s*\[S\d+\]`)

type ChatService struct {
	sourceIndexes SourceIndexReader
	sharedCorpus  bool
	chats         domain.ChatCatalog
	llm           domain.Answerer
}

func NewChatService(chats domain.ChatCatalog, llm domain.Answerer) *ChatService {
	return &ChatService{chats: chats, llm: llm}
}

func (s *ChatService) CreateChat(ctx context.Context, ownerID, title, source string) (*commonv1.Chat, error) {
	return s.chats.CreateChat(ctx, ownerID, title, source)
}

func (s *ChatService) ListChats(
	ctx context.Context, ownerID string, limit, offset int, allOwners bool,
) ([]*commonv1.Chat, int64, error) {
	return s.chats.ListChats(ctx, ownerID, limit, offset, allOwners)
}

func (s *ChatService) GetChat(ctx context.Context, ownerID, chatID string) (*commonv1.Chat, error) {
	chat, err := s.chats.GetChat(ctx, chatID)
	if err != nil {
		return nil, err
	}
	if chat.GetOwnerId() != ownerID {
		return nil, ErrForbidden
	}
	return chat, nil
}

func (s *ChatService) ListMessages(ctx context.Context, ownerID, chatID string) ([]*commonv1.Message, error) {
	return s.MessagePage(ctx, ownerID, chatID, "", 100)
}

// MessagePage проверяет владельца и источники каждой возвращаемой страницы.
func (s *ChatService) MessagePage(ctx context.Context, ownerID, chatID, before string, limit int) ([]*commonv1.Message, error) {
	if _, err := s.GetChat(ctx, ownerID, chatID); err != nil {
		return nil, err
	}
	messages, err := s.chats.MessagePage(ctx, chatID, before, limit)
	if err != nil {
		return nil, err
	}
	if err = s.checkSourceHistory(ctx, ownerID, messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func (s *ChatService) ListModels(ctx context.Context) ([]*commonv1.Model, error) {
	return s.chats.ListModels(ctx)
}

func (s *ChatService) Ask(ctx context.Context, cmd domain.AskCommand) (*commonv1.Message, error) {
	ctx, cancel := withDeadline(ctx, llmSinglePassTimeout)
	defer cancel()
	log := logger.From(ctx)

	// Владелец проверяется до первой записи, иначе ход ляжет в чужой чат.
	if _, err := s.GetChat(ctx, cmd.OwnerID, cmd.ChatID); err != nil {
		return nil, err
	}

	turn, err := s.chats.BeginChatTurn(ctx, &dbv1.BeginChatTurnRequest{
		OwnerId: cmd.OwnerID, ChatId: cmd.ChatID, RequestId: cmd.RequestID, Content: cmd.Content,
	})
	if err != nil {
		return nil, err
	}
	if completed := turn.GetCompleted(); completed != nil {
		if err = s.checkSourceHistory(ctx, cmd.OwnerID, []*commonv1.Message{completed}); err != nil {
			return nil, err
		}
		return completed, nil
	}
	finished := false
	defer func() {
		if finished {
			return
		}
		// Завершение аренды должно пережить отмену HTTP, но не зависнуть при отказе БД.
		cleanup, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancelCleanup()
		if failure := s.chats.FailChatTurn(cleanup, turn.GetLease()); failure != nil {
			log.Error().Err(failure).Str("chat_id", cmd.ChatID).Msg("chat turn release failed")
		}
	}()
	history := turn.GetHistory()
	if err = s.checkSourceHistory(ctx, cmd.OwnerID, history); err != nil {
		return nil, err
	}

	resp, err := s.llm.Answer(ctx, &commonv1.RagRequest{
		OwnerId: cmd.OwnerID,
		Query:   contextualQuery(history, cmd.Content),
		TopK:    adaptiveTopK(cmd.Content, defaultTopK, chatTopKMax),
		Stream:  false,
	})
	if err != nil {
		return nil, fmt.Errorf("rag answer: %w", err)
	}

	if err = s.checkSourceHistory(ctx, cmd.OwnerID, []*commonv1.Message{{Sources: resp.GetSources()}}); err != nil {
		return nil, err
	}
	assistant, err := s.chats.FinishChatTurn(ctx, &dbv1.FinishChatTurnRequest{
		Lease: turn.GetLease(), Content: resp.GetAnswer(), Sources: resp.GetSources(), Meta: answerMeta(resp),
	})
	if err != nil {
		return nil, fmt.Errorf("persist assistant message: %w", err)
	}
	finished = true
	log.Info().
		Str("chat_id", cmd.ChatID).
		Int("sources", len(resp.GetSources())).
		Bool("cached", resp.GetCached()).
		Msg("chat turn answered")
	return assistant, nil
}

func contextualQuery(history []*commonv1.Message, question string) string {
	questions, lastAnswer := dialogContext(history)
	if len(questions) == 0 && lastAnswer == "" {
		return question
	}
	var b strings.Builder
	b.WriteString(question)
	b.WriteString("\n\nКонтекст диалога для понимания вопроса:")
	for _, q := range questions {
		b.WriteString("\nРанее спрашивали: ")
		b.WriteString(q)
	}
	if lastAnswer != "" {
		b.WriteString("\nПоследний ответ (начало): ")
		b.WriteString(lastAnswer)
	}
	return b.String()
}

func dialogContext(history []*commonv1.Message) ([]string, string) {
	questions := make([]string, 0, historyQuestionsMax)
	lastAnswer := ""
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		text := strings.TrimSpace(m.GetContent())
		if text == "" {
			continue
		}
		switch m.GetRole() {
		case "user":
			if len(questions) < historyQuestionsMax {
				q := strings.TrimSpace(strings.ReplaceAll(text, "?", " "))
				questions = append(questions, truncateRunes(q, historyQuestionClip))
			}
		case "assistant":
			if lastAnswer == "" {
				clean := strings.Join(strings.Fields(citationLabelRe.ReplaceAllString(text, " ")), " ")
				lastAnswer = truncateRunes(clean, historyAnswerClip)
			}
		}
		if len(questions) == historyQuestionsMax && lastAnswer != "" {
			break
		}
	}
	slices.Reverse(questions)
	return questions, lastAnswer
}

// answerMeta: трасса хранится в protojson; читать её с DiscardUnknown, чтобы конверт пережил новые поля.
func answerMeta(resp *commonv1.RagResponse) string {
	env := struct {
		Model  string          `json:"model"`
		Cached bool            `json:"cached"`
		Trace  json.RawMessage `json:"trace,omitempty"`
	}{Model: resp.GetModel(), Cached: resp.GetCached()}
	if tr := resp.GetTrace(); tr != nil {
		if raw, err := (protojson.MarshalOptions{UseProtoNames: true}).Marshal(tr); err == nil {
			env.Trace = raw
		}
	}
	raw, err := json.Marshal(env)
	if err != nil {
		return ""
	}
	return string(raw)
}
