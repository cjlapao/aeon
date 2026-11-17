-- How a route's speech endpoints are spoken: the OpenAI audio API (the default), the synthesize API, or the chat
-- completions of an omni model that hears and speaks in one. NULL means the default, so every route saved before this
-- column existed keeps working exactly as it did.
ALTER TABLE route ADD COLUMN audio_api TEXT;
