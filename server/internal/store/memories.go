package store

import (
	"context"
	"database/sql"
	"encoding/binary"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"strings"
	"unicode/utf8"
)

type Memory struct {
	ID              string    `json:"id"`
	UserID          string    `json:"-"`
	AssistantID     *string   `json:"-"`
	Category        string    `json:"category"`
	Content         string    `json:"content"`
	Keywords        string    `json:"-"`
	KeywordsChecked bool      `json:"-"`
	Embedding       []float32 `json:"-"`
	EmbeddingModel  string    `json:"-"`
	Source          string    `json:"source"`
	SourceConvID    *string   `json:"source_conv_id"`
	Status          string    `json:"-"`
	Pinned          bool      `json:"pinned"`
	SupersededBy    *string   `json:"-"`
	ValidUntil      *int64    `json:"-"`
	CreatedAt       int64     `json:"created_at"`
	UpdatedAt       int64     `json:"updated_at"`
}

func (m Memory) MarshalJSON() ([]byte, error) {
	type plain Memory
	return json.Marshal(struct {
		plain
		IsNew bool `json:"is_new"`
	}{plain(m), m.Source == "auto" && Now()-m.CreatedAt < 3*24*3600*1000})
}

func ValidMemoryCategory(c string, episode bool) bool {
	switch c {
	case "health", "family", "profile", "preference", "ongoing":
		return true
	case "episode":
		return episode
	}
	return false
}

func EncodeEmbedding(v []float32) []byte {
	if len(v) == 0 {
		return nil
	}
	b := make([]byte, 4*len(v))
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}
func decodeEmbedding(b []byte) []float32 {
	if len(b) == 0 || len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

const memoryCols = `id, user_id, assistant_id, category, content, keywords, keywords_checked, embedding, embedding_model, source, source_conv_id, status, pinned, superseded_by, valid_until, created_at, updated_at`

func scanMemory(row interface{ Scan(...any) error }) (*Memory, error) {
	var m Memory
	var b []byte
	if err := row.Scan(&m.ID, &m.UserID, &m.AssistantID, &m.Category, &m.Content, &m.Keywords, &m.KeywordsChecked, &b, &m.EmbeddingModel, &m.Source, &m.SourceConvID, &m.Status, &m.Pinned, &m.SupersededBy, &m.ValidUntil, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, notFound(err)
	}
	m.Embedding = decodeEmbedding(b)
	return &m, nil
}
func insertMemory(ctx context.Context, db execer, m *Memory) error {
	if m.ID == "" {
		m.ID = NewID()
	}
	if m.CreatedAt == 0 {
		m.CreatedAt = Now()
	}
	m.UpdatedAt = Now()
	if m.Status == "" {
		m.Status = "active"
	}
	if m.Source == "" {
		m.Source = "auto"
	}
	if strings.TrimSpace(m.Keywords) != "" {
		m.KeywordsChecked = true
	}
	if m.Category == "episode" {
		m.Pinned = false
	}
	_, err := db.exec(ctx, `INSERT INTO memories (`+memoryCols+`) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, m.ID, m.UserID, m.AssistantID, m.Category, m.Content, m.Keywords, m.KeywordsChecked, EncodeEmbedding(m.Embedding), m.EmbeddingModel, m.Source, m.SourceConvID, m.Status, m.Pinned, m.SupersededBy, m.ValidUntil, m.CreatedAt, m.UpdatedAt)
	return err
}
func (s *Store) CreateMemories(ctx context.Context, ms []*Memory) error {
	return s.tx(ctx, func(tx *Tx) error {
		for _, m := range ms {
			if err := insertMemory(ctx, tx, m); err != nil {
				return err
			}
		}
		return nil
	})
}
func (s *Store) GetMemory(ctx context.Context, id, user string) (*Memory, error) {
	return scanMemory(s.queryRow(ctx, `SELECT `+memoryCols+` FROM memories WHERE id=? AND user_id=? AND status='active'`, id, user))
}

const memoryOrder = `CASE category WHEN 'health' THEN 0 WHEN 'family' THEN 1 WHEN 'profile' THEN 2 WHEN 'preference' THEN 3 WHEN 'ongoing' THEN 4 ELSE 5 END, created_at DESC, id DESC`

func readMemories(rows *sql.Rows, err error) ([]*Memory, error) {
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Memory{}
	for rows.Next() {
		m, err := scanMemory(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *Store) ListMemories(ctx context.Context, user string) ([]*Memory, error) {
	return readMemories(s.query(ctx, `SELECT `+memoryCols+` FROM memories WHERE user_id=? AND status='active' ORDER BY `+memoryOrder, user))
}
func (s *Store) UpdateMemory(ctx context.Context, id, user string, category, content *string) (*Memory, error) {
	err := s.tx(ctx, func(tx *Tx) error {
		m, err := scanMemory(tx.queryRow(ctx, `SELECT `+memoryCols+` FROM memories WHERE id=? AND user_id=? AND status='active'`+s.forUpdate(), id, user))
		if err != nil {
			return err
		}
		if category != nil {
			m.Category = *category
			m.Pinned = m.Category != "episode"
		}
		if content != nil {
			m.Content = *content
		}
		return affected(tx.exec(ctx, `UPDATE memories SET category=?,content=?,keywords='',keywords_checked=0,embedding=NULL,embedding_model='',pinned=?,updated_at=? WHERE id=? AND user_id=?`, m.Category, m.Content, m.Pinned, Now(), id, user))
	})
	if err != nil {
		return nil, err
	}
	return s.GetMemory(ctx, id, user)
}
func (s *Store) DeleteMemory(ctx context.Context, id, user string) error {
	return affected(s.exec(ctx, `DELETE FROM memories WHERE id=? AND user_id=?`, id, user))
}
func (s *Store) ClearMemories(ctx context.Context, user string) error {
	_, err := s.exec(ctx, `DELETE FROM memories WHERE user_id=?`, user)
	return err
}
func (s *Store) PendingKeywords(ctx context.Context, limit int) ([]*Memory, error) {
	if limit <= 0 || limit > 100 {
		limit = 25
	}
	return readMemories(s.query(ctx, `SELECT `+memoryCols+` FROM memories WHERE status='active' AND keywords_checked=0 ORDER BY updated_at DESC,id DESC LIMIT ?`, limit))
}

// SetMemoryKeywords attaches generated synonym keywords unless the memory was edited meanwhile.
// When keywords change, any stale vector is cleared so FillEmbeddings re-embeds with the new terms.
func (s *Store) SetMemoryKeywords(ctx context.Context, m *Memory, keywords string) error {
	keywords = strings.TrimSpace(keywords)
	if keywords == strings.TrimSpace(m.Keywords) {
		_, err := s.exec(ctx, `UPDATE memories SET keywords_checked=1 WHERE id=? AND content=? AND updated_at=? AND status='active'`, m.ID, m.Content, m.UpdatedAt)
		return err
	}
	_, err := s.exec(ctx, `UPDATE memories SET keywords=?,keywords_checked=1,embedding=NULL,embedding_model='',updated_at=? WHERE id=? AND content=? AND updated_at=? AND status='active'`, keywords, Now(), m.ID, m.Content, m.UpdatedAt)
	return err
}

func (s *Store) PendingEmbeddings(ctx context.Context, model string) ([]*Memory, error) {
	return readMemories(s.query(ctx, `SELECT `+memoryCols+` FROM memories WHERE status='active' AND (embedding IS NULL OR embedding_model<>?) ORDER BY updated_at,id LIMIT 32`, model))
}

// SetMemoryEmbedding does not overwrite content edited while embedding was in flight.
func (s *Store) SetMemoryEmbedding(ctx context.Context, m *Memory, v []float32, model string) error {
	_, err := s.exec(ctx, `UPDATE memories SET embedding=?,embedding_model=? WHERE id=? AND content=? AND updated_at=? AND status='active'`, EncodeEmbedding(v), model, m.ID, m.Content, m.UpdatedAt)
	return err
}

type MemoryStats struct {
	Active          int
	Embedded        int
	PendingKeywords int
}

func (s *Store) MemoryStats(ctx context.Context, embeddingModel string) (MemoryStats, error) {
	var st MemoryStats
	err := s.queryRow(ctx, `SELECT COUNT(*), COUNT(CASE WHEN ? <> '' AND embedding IS NOT NULL AND embedding_model = ? THEN 1 END), COUNT(CASE WHEN keywords_checked = 0 THEN 1 END) FROM memories WHERE status = 'active'`, embeddingModel, embeddingModel).
		Scan(&st.Active, &st.Embedded, &st.PendingKeywords)
	return st, err
}

// MemorySnapshot captures exactly the segment that an extraction may commit.
type MemorySnapshot struct {
	ConversationID, UserID, Cursor string
	Messages                       []*Message
}
type MemoryChange struct {
	Op, RefID string
	Memory    *Memory
}
type EpisodeSpec struct {
	Content  string
	Keywords string
}

var ErrMemoryStale = errors.New("memory snapshot changed")

func (s *Store) MemoryCursor(ctx context.Context, conv string) (string, error) {
	var c string
	err := s.queryRow(ctx, `SELECT memory_cursor FROM conversations WHERE id=?`, conv).Scan(&c)
	return c, notFound(err)
}
func (s *Store) MemoryMessages(ctx context.Context, conv, cursor string) ([]*Message, error) {
	rows, err := s.query(ctx, `SELECT `+msgCols+` FROM messages WHERE conversation_id=? AND id>? ORDER BY id LIMIT 100`, conv, cursor)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Message{}
	for rows.Next() {
		m, err := scanMsg(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
func (s *Store) MemoryCandidates(ctx context.Context, before int64) ([]*Conversation, error) {
	rows, err := s.query(ctx, `SELECT `+prefixCols(convCols, "c.")+` FROM conversations c WHERE c.private=0 AND c.updated_at<=? AND EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id=c.id AND m.id>c.memory_cursor) AND NOT EXISTS (SELECT 1 FROM messages m WHERE m.conversation_id=c.id AND m.status='streaming') ORDER BY c.updated_at,c.id`, before)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Conversation{}
	for rows.Next() {
		c, err := scanConv(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ApplyMemoryExtraction commits changes and at most a single episode summary.
func (s *Store) ApplyMemoryExtraction(ctx context.Context, snap MemorySnapshot, changes []MemoryChange, episode string) error {
	var eps []EpisodeSpec
	if strings.TrimSpace(episode) != "" {
		eps = []EpisodeSpec{{Content: strings.TrimSpace(episode)}}
	}
	return s.ApplyMemoryExtractionEpisodes(ctx, snap, changes, eps)
}

// ApplyMemoryExtractionEpisodes locks the same conversation row as ApplyTurn before validating,
// and reconciles up to 3 topic-level episode summaries for the conversation.
func (s *Store) ApplyMemoryExtractionEpisodes(ctx context.Context, snap MemorySnapshot, changes []MemoryChange, episodes []EpisodeSpec) error {
	return s.tx(ctx, func(tx *Tx) error {
		// Serialize a concurrent settings toggle with the commit, not the model call.
		u, err := scanUser(tx.queryRow(ctx, `SELECT `+userCols+` FROM users WHERE id=?`+s.forUpdate(), snap.UserID))
		if err != nil {
			return err
		}
		var settings map[string]any
		_ = json.Unmarshal(u.Settings, &settings)
		enabled := settings["memory_enabled"] != false
		var assistant *string
		var cursor string
		var isPrivate bool
		if err := tx.queryRow(ctx, `SELECT memory_cursor, assistant_id, private FROM conversations WHERE id=? AND user_id=?`+s.forUpdate(), snap.ConversationID, snap.UserID).Scan(&cursor, &assistant, &isPrivate); err != nil {
			return notFound(err)
		}
		if isPrivate {
			return nil
		}
		if assistant != nil {
			var use bool
			if err := tx.queryRow(ctx, `SELECT use_memory FROM assistants WHERE id=?`, *assistant).Scan(&use); err != nil {
				return notFound(err)
			}
			enabled = enabled && use
		}
		if !enabled {
			changes = nil
			episodes = nil
		}
		if cursor != snap.Cursor || len(snap.Messages) == 0 {
			return ErrMemoryStale
		}
		for _, m := range snap.Messages {
			actual, err := scanMsg(tx.queryRow(ctx, `SELECT `+msgCols+` FROM messages WHERE id=? AND conversation_id=?`, m.ID, snap.ConversationID))
			if err != nil || !reflect.DeepEqual(actual, m) {
				return ErrMemoryStale
			}
		}
		for _, c := range changes {
			if c.Op == "add" {
				if c.Memory.UserID != snap.UserID {
					return ErrMemoryStale
				}
				if err := insertMemory(ctx, tx, c.Memory); err != nil {
					return err
				}
				continue
			}
			status := "archived"
			var next any
			if c.Op == "update" {
				status = "superseded"
				c.Memory.ID = NewID()
				next = c.Memory.ID
			}
			res, err := tx.exec(ctx, `UPDATE memories SET status=?,superseded_by=?,updated_at=? WHERE id=? AND user_id=? AND status='active'`, status, next, Now(), c.RefID, snap.UserID)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n == 0 {
				continue
			}
			if c.Op == "update" {
				if err := insertMemory(ctx, tx, c.Memory); err != nil {
					return err
				}
			}
		}
		if len(episodes) > 0 {
			if len(episodes) > 3 {
				episodes = episodes[:3]
			}
			rows, err := tx.query(ctx, `SELECT `+memoryCols+` FROM memories WHERE source_conv_id=? AND user_id=? AND category='episode' AND status='active' ORDER BY created_at,id`+s.forUpdate(), snap.ConversationID, snap.UserID)
			existingEps, err := readMemories(rows, err)
			if err != nil {
				return err
			}
			now := Now()
			for i, ep := range episodes {
				ep.Content = strings.TrimSpace(ep.Content)
				ep.Keywords = strings.TrimSpace(ep.Keywords)
				if ep.Content == "" {
					continue
				}
				checked := ep.Keywords != ""
				if i < len(existingEps) {
					old := existingEps[i]
					if old.Content == ep.Content && (ep.Keywords == "" || old.Keywords == ep.Keywords) {
						continue
					}
					kw := ep.Keywords
					if kw == "" && old.Content == ep.Content {
						kw = old.Keywords
						checked = old.KeywordsChecked
					}
					if _, err := tx.exec(ctx, `UPDATE memories SET content=?,keywords=?,keywords_checked=?,embedding=NULL,embedding_model='',updated_at=? WHERE id=?`, ep.Content, kw, checked, now, old.ID); err != nil {
						return err
					}
				} else {
					m := &Memory{UserID: snap.UserID, Category: "episode", Content: ep.Content, Keywords: ep.Keywords, KeywordsChecked: checked, SourceConvID: &snap.ConversationID}
					if err := insertMemory(ctx, tx, m); err != nil {
						return err
					}
				}
			}
			for i := len(episodes); i < len(existingEps); i++ {
				if _, err := tx.exec(ctx, `UPDATE memories SET status='archived',updated_at=? WHERE id=?`, now, existingEps[i].ID); err != nil {
					return err
				}
			}
		}
		if _, err := tx.exec(ctx, `UPDATE conversations SET memory_cursor=? WHERE id=?`, snap.Messages[len(snap.Messages)-1].ID, snap.ConversationID); err != nil {
			return err
		}
		if !enabled {
			return nil
		}
		return demoteMemories(ctx, tx, snap.UserID)
	})
}
func demoteMemories(ctx context.Context, tx *Tx, user string) error {
	rows, err := tx.query(ctx, `SELECT `+memoryCols+` FROM memories WHERE user_id=? AND status='active' AND pinned=1 ORDER BY CASE category WHEN 'ongoing' THEN 0 WHEN 'preference' THEN 1 ELSE 2 END,created_at,id`, user)
	ms, err := readMemories(rows, err)
	if err != nil {
		return err
	}
	n := 0
	for _, m := range ms {
		n += utf8.RuneCountInString(m.Content)
	}
	for _, m := range ms {
		if n <= 1500 {
			break
		}
		if m.Category != "ongoing" && m.Category != "preference" {
			continue
		}
		if _, err := tx.exec(ctx, `UPDATE memories SET pinned=0,updated_at=? WHERE id=?`, Now(), m.ID); err != nil {
			return err
		}
		n -= utf8.RuneCountInString(m.Content)
	}
	return nil
}
