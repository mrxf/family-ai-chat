-- v3: tool calling and image models.

-- 'chat' models answer the family; 'image' models only draw pictures for the
-- generate_image tool and never appear in the family's model list.
ALTER TABLE models ADD COLUMN kind TEXT NOT NULL DEFAULT 'chat';

-- The chat model may call tools (draw pictures, later web search). On by default:
-- an upstream that rejects tools is retried without them.
ALTER TABLE models ADD COLUMN tools BIGINT NOT NULL DEFAULT 1;
