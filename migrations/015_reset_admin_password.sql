-- Reset admin password to: password123
-- bcrypt cost 12 hash
UPDATE users
SET password_hash = '$2b$12$Ed4pzXUwlbqP04nI3anpau/DGDFeAK4A9GWSqt9NYEHl3yADDRu9a'
WHERE email = 'admin@orbit.local';
