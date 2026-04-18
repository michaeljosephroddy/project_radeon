ALTER TABLE conversations RENAME TO chats;
ALTER TABLE conversation_members RENAME TO chat_members;

ALTER TABLE chat_members RENAME COLUMN conversation_id TO chat_id;
ALTER TABLE messages RENAME COLUMN conversation_id TO chat_id;

ALTER TABLE chats RENAME CONSTRAINT conversations_pkey TO chats_pkey;
ALTER TABLE chat_members RENAME CONSTRAINT conversation_members_pkey TO chat_members_pkey;
ALTER TABLE chat_members RENAME CONSTRAINT conversation_members_conversation_id_fkey TO chat_members_chat_id_fkey;
ALTER TABLE chat_members RENAME CONSTRAINT conversation_members_user_id_fkey TO chat_members_user_id_fkey;
ALTER TABLE messages RENAME CONSTRAINT messages_conversation_id_fkey TO messages_chat_id_fkey;

ALTER INDEX idx_messages_conversation_id RENAME TO idx_messages_chat_id;
