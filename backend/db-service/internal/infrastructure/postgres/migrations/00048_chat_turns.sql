-- +goose Up
CREATE TABLE chat_turns (
    chat_id text NOT NULL REFERENCES chats(id) ON DELETE CASCADE,
    request_id text NOT NULL CHECK (length(request_id) BETWEEN 1 AND 128),
    sequence bigint NOT NULL CHECK (sequence > 0),
    content text NOT NULL CHECK (octet_length(content) BETWEEN 1 AND 65536),
    state text NOT NULL CHECK (state IN ('pending', 'completed', 'failed')),
    token text NOT NULL,
    lease_until timestamptz NOT NULL,
    user_message_id text NOT NULL UNIQUE REFERENCES chat_messages(id) ON DELETE CASCADE,
    assistant_message_id text UNIQUE REFERENCES chat_messages(id),
    failure_code text NOT NULL DEFAULT '',
    PRIMARY KEY (chat_id, request_id),
    UNIQUE (chat_id, sequence)
);
CREATE UNIQUE INDEX chat_turns_active ON chat_turns(chat_id) WHERE state='pending';
CREATE INDEX chat_messages_page ON chat_messages(chat_id, created_at DESC, id DESC);

-- +goose Down
DROP TABLE chat_turns;
DROP INDEX chat_messages_page;
