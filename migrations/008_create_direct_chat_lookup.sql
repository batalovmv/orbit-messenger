CREATE TABLE direct_chat_lookup (
    user1_id    UUID NOT NULL,
    user2_id    UUID NOT NULL,
    chat_id     UUID NOT NULL REFERENCES chats(id),
    PRIMARY KEY (user1_id, user2_id),
    CONSTRAINT direct_chat_canonical_order CHECK (user1_id < user2_id)
);
