-- Never-configured sites use the homepage by default. Preserve every explicit
-- access choice and all existing node display memberships.
UPDATE public_page SET enabled = 1 WHERE singleton_id = 1 AND updated_at = 0;
