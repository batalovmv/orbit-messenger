--
-- PostgreSQL database dump
--

\restrict 18gVG98M1tcavCFdT5IKvnEsjzdeav52Y77MrIrTez6Obkl1e7edluEVj80M1OA

-- Dumped from database version 16.11
-- Dumped by pg_dump version 16.11

SET statement_timeout = 0;
SET lock_timeout = 0;
SET idle_in_transaction_session_timeout = 0;
SET client_encoding = 'UTF8';
SET standard_conforming_strings = on;
SELECT pg_catalog.set_config('search_path', '', false);
SET check_function_bodies = false;
SET xmloption = content;
SET client_min_messages = warning;
SET row_security = off;

ALTER TABLE IF EXISTS ONLY public.users DROP CONSTRAINT IF EXISTS users_invited_by_fkey;
ALTER TABLE IF EXISTS ONLY public.sessions DROP CONSTRAINT IF EXISTS sessions_user_id_fkey;
ALTER TABLE IF EXISTS ONLY public.sessions DROP CONSTRAINT IF EXISTS sessions_device_id_fkey;
ALTER TABLE IF EXISTS ONLY public.messages DROP CONSTRAINT IF EXISTS messages_sender_id_fkey;
ALTER TABLE IF EXISTS ONLY public.messages DROP CONSTRAINT IF EXISTS messages_reply_to_id_fkey;
ALTER TABLE IF EXISTS ONLY public.messages DROP CONSTRAINT IF EXISTS messages_forwarded_from_fkey;
ALTER TABLE IF EXISTS ONLY public.messages DROP CONSTRAINT IF EXISTS messages_chat_id_fkey;
ALTER TABLE IF EXISTS ONLY public.invites DROP CONSTRAINT IF EXISTS invites_used_by_fkey;
ALTER TABLE IF EXISTS ONLY public.invites DROP CONSTRAINT IF EXISTS invites_created_by_fkey;
ALTER TABLE IF EXISTS ONLY public.direct_chat_lookup DROP CONSTRAINT IF EXISTS direct_chat_lookup_user2_fkey;
ALTER TABLE IF EXISTS ONLY public.direct_chat_lookup DROP CONSTRAINT IF EXISTS direct_chat_lookup_user1_fkey;
ALTER TABLE IF EXISTS ONLY public.direct_chat_lookup DROP CONSTRAINT IF EXISTS direct_chat_lookup_chat_id_fkey;
ALTER TABLE IF EXISTS ONLY public.devices DROP CONSTRAINT IF EXISTS devices_user_id_fkey;
ALTER TABLE IF EXISTS ONLY public.chats DROP CONSTRAINT IF EXISTS chats_created_by_fkey;
ALTER TABLE IF EXISTS ONLY public.chat_members DROP CONSTRAINT IF EXISTS chat_members_user_id_fkey;
ALTER TABLE IF EXISTS ONLY public.chat_members DROP CONSTRAINT IF EXISTS chat_members_last_read_message_id_fkey;
ALTER TABLE IF EXISTS ONLY public.chat_members DROP CONSTRAINT IF EXISTS chat_members_chat_id_fkey;
DROP TRIGGER IF EXISTS trg_users_updated_at ON public.users;
DROP TRIGGER IF EXISTS trg_chats_updated_at ON public.chats;
DROP INDEX IF EXISTS public.idx_users_email;
DROP INDEX IF EXISTS public.idx_sessions_user;
DROP INDEX IF EXISTS public.idx_sessions_token;
DROP INDEX IF EXISTS public.idx_messages_chat_seq;
DROP INDEX IF EXISTS public.idx_messages_chat_created;
DROP INDEX IF EXISTS public.idx_devices_user;
DROP INDEX IF EXISTS public.idx_chat_members_user;
ALTER TABLE IF EXISTS ONLY public.users DROP CONSTRAINT IF EXISTS users_pkey;
ALTER TABLE IF EXISTS ONLY public.users DROP CONSTRAINT IF EXISTS users_phone_key;
ALTER TABLE IF EXISTS ONLY public.users DROP CONSTRAINT IF EXISTS users_email_key;
ALTER TABLE IF EXISTS ONLY public.sessions DROP CONSTRAINT IF EXISTS sessions_pkey;
ALTER TABLE IF EXISTS ONLY public.messages DROP CONSTRAINT IF EXISTS messages_pkey;
ALTER TABLE IF EXISTS ONLY public.invites DROP CONSTRAINT IF EXISTS invites_pkey;
ALTER TABLE IF EXISTS ONLY public.invites DROP CONSTRAINT IF EXISTS invites_code_key;
ALTER TABLE IF EXISTS ONLY public.direct_chat_lookup DROP CONSTRAINT IF EXISTS direct_chat_lookup_pkey;
ALTER TABLE IF EXISTS ONLY public.devices DROP CONSTRAINT IF EXISTS devices_pkey;
ALTER TABLE IF EXISTS ONLY public.chats DROP CONSTRAINT IF EXISTS chats_pkey;
ALTER TABLE IF EXISTS ONLY public.chat_members DROP CONSTRAINT IF EXISTS chat_members_pkey;
DROP TABLE IF EXISTS public.users;
DROP TABLE IF EXISTS public.sessions;
DROP TABLE IF EXISTS public.messages;
DROP SEQUENCE IF EXISTS public.messages_seq;
DROP TABLE IF EXISTS public.invites;
DROP TABLE IF EXISTS public.direct_chat_lookup;
DROP TABLE IF EXISTS public.devices;
DROP TABLE IF EXISTS public.chats;
DROP TABLE IF EXISTS public.chat_members;
DROP FUNCTION IF EXISTS public.update_updated_at();
DROP EXTENSION IF EXISTS pgcrypto;
--
-- Name: pgcrypto; Type: EXTENSION; Schema: -; Owner: -
--

CREATE EXTENSION IF NOT EXISTS pgcrypto WITH SCHEMA public;


--
-- Name: EXTENSION pgcrypto; Type: COMMENT; Schema: -; Owner: 
--

COMMENT ON EXTENSION pgcrypto IS 'cryptographic functions';


--
-- Name: update_updated_at(); Type: FUNCTION; Schema: public; Owner: orbit
--

CREATE FUNCTION public.update_updated_at() RETURNS trigger
    LANGUAGE plpgsql
    AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;


ALTER FUNCTION public.update_updated_at() OWNER TO orbit;

SET default_tablespace = '';

SET default_table_access_method = heap;

--
-- Name: chat_members; Type: TABLE; Schema: public; Owner: orbit
--

CREATE TABLE public.chat_members (
    chat_id uuid NOT NULL,
    user_id uuid NOT NULL,
    role text DEFAULT 'member'::text NOT NULL,
    last_read_message_id uuid,
    joined_at timestamp with time zone DEFAULT now() NOT NULL,
    muted_until timestamp with time zone,
    notification_level text DEFAULT 'all'::text NOT NULL,
    CONSTRAINT chat_members_notification_level_check CHECK ((notification_level = ANY (ARRAY['all'::text, 'mentions'::text, 'none'::text]))),
    CONSTRAINT chat_members_role_check CHECK ((role = ANY (ARRAY['owner'::text, 'admin'::text, 'member'::text, 'readonly'::text, 'banned'::text])))
);


ALTER TABLE public.chat_members OWNER TO orbit;

--
-- Name: chats; Type: TABLE; Schema: public; Owner: orbit
--

CREATE TABLE public.chats (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    type text NOT NULL,
    name text,
    description text,
    avatar_url text,
    created_by uuid,
    is_encrypted boolean DEFAULT false NOT NULL,
    max_members integer DEFAULT 200000 NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT chats_type_check CHECK ((type = ANY (ARRAY['direct'::text, 'group'::text, 'channel'::text])))
);


ALTER TABLE public.chats OWNER TO orbit;

--
-- Name: devices; Type: TABLE; Schema: public; Owner: orbit
--

CREATE TABLE public.devices (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    device_name text,
    device_type text,
    identity_key bytea,
    push_token text,
    push_type text,
    last_active_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT devices_device_type_check CHECK ((device_type = ANY (ARRAY['web'::text, 'desktop'::text, 'ios'::text, 'android'::text]))),
    CONSTRAINT devices_push_type_check CHECK ((push_type = ANY (ARRAY['vapid'::text, 'fcm'::text, 'apns'::text])))
);


ALTER TABLE public.devices OWNER TO orbit;

--
-- Name: direct_chat_lookup; Type: TABLE; Schema: public; Owner: orbit
--

CREATE TABLE public.direct_chat_lookup (
    user1_id uuid NOT NULL,
    user2_id uuid NOT NULL,
    chat_id uuid NOT NULL,
    CONSTRAINT direct_chat_canonical_order CHECK ((user1_id < user2_id))
);


ALTER TABLE public.direct_chat_lookup OWNER TO orbit;

--
-- Name: invites; Type: TABLE; Schema: public; Owner: orbit
--

CREATE TABLE public.invites (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    code text NOT NULL,
    created_by uuid,
    email text,
    role text DEFAULT 'member'::text NOT NULL,
    max_uses integer DEFAULT 1 NOT NULL,
    use_count integer DEFAULT 0 NOT NULL,
    used_by uuid,
    used_at timestamp with time zone,
    expires_at timestamp with time zone,
    is_active boolean DEFAULT true NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT invites_role_check CHECK ((role = ANY (ARRAY['admin'::text, 'member'::text])))
);


ALTER TABLE public.invites OWNER TO orbit;

--
-- Name: messages_seq; Type: SEQUENCE; Schema: public; Owner: orbit
--

CREATE SEQUENCE public.messages_seq
    START WITH 1
    INCREMENT BY 1
    NO MINVALUE
    NO MAXVALUE
    CACHE 1;


ALTER SEQUENCE public.messages_seq OWNER TO orbit;

--
-- Name: messages; Type: TABLE; Schema: public; Owner: orbit
--

CREATE TABLE public.messages (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    chat_id uuid NOT NULL,
    sender_id uuid,
    type text DEFAULT 'text'::text NOT NULL,
    content text,
    encrypted_content bytea,
    reply_to_id uuid,
    is_edited boolean DEFAULT false NOT NULL,
    is_deleted boolean DEFAULT false NOT NULL,
    is_pinned boolean DEFAULT false NOT NULL,
    is_forwarded boolean DEFAULT false NOT NULL,
    forwarded_from uuid,
    thread_id uuid,
    expires_at timestamp with time zone,
    sequence_number bigint DEFAULT nextval('public.messages_seq'::regclass) NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    edited_at timestamp with time zone,
    entities jsonb,
    CONSTRAINT messages_type_check CHECK ((type = ANY (ARRAY['text'::text, 'photo'::text, 'video'::text, 'file'::text, 'voice'::text, 'videonote'::text, 'sticker'::text, 'poll'::text, 'system'::text])))
);


ALTER TABLE public.messages OWNER TO orbit;

--
-- Name: sessions; Type: TABLE; Schema: public; Owner: orbit
--

CREATE TABLE public.sessions (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    user_id uuid NOT NULL,
    device_id uuid,
    token_hash text NOT NULL,
    ip_address inet,
    user_agent text,
    expires_at timestamp with time zone NOT NULL,
    created_at timestamp with time zone DEFAULT now() NOT NULL
);


ALTER TABLE public.sessions OWNER TO orbit;

--
-- Name: users; Type: TABLE; Schema: public; Owner: orbit
--

CREATE TABLE public.users (
    id uuid DEFAULT gen_random_uuid() NOT NULL,
    email text NOT NULL,
    password_hash text NOT NULL,
    phone text,
    display_name text NOT NULL,
    avatar_url text,
    bio text,
    status text DEFAULT 'offline'::text NOT NULL,
    custom_status text,
    custom_status_emoji text,
    role text DEFAULT 'member'::text NOT NULL,
    totp_secret text,
    totp_enabled boolean DEFAULT false NOT NULL,
    invited_by uuid,
    invite_code text,
    last_seen_at timestamp with time zone,
    created_at timestamp with time zone DEFAULT now() NOT NULL,
    updated_at timestamp with time zone DEFAULT now() NOT NULL,
    CONSTRAINT users_role_check CHECK ((role = ANY (ARRAY['admin'::text, 'member'::text]))),
    CONSTRAINT users_status_check CHECK ((status = ANY (ARRAY['online'::text, 'offline'::text, 'recently'::text])))
);


ALTER TABLE public.users OWNER TO orbit;

--
-- Data for Name: chat_members; Type: TABLE DATA; Schema: public; Owner: orbit
--

COPY public.chat_members (chat_id, user_id, role, last_read_message_id, joined_at, muted_until, notification_level) FROM stdin;
1a983ce5-170c-452c-8217-221a5a5f3bd8	4363c0f0-845c-4605-8085-5eb5afef1426	owner	\N	2026-03-26 04:16:10.60566+00	\N	all
55840b43-b9ea-4d7c-abe5-2ee892f27a73	4363c0f0-845c-4605-8085-5eb5afef1426	member	99f479dc-d57d-4b0a-8b5b-0d7447c7fdd5	2026-03-26 04:08:11.771051+00	\N	all
55840b43-b9ea-4d7c-abe5-2ee892f27a73	7bbb72d1-9635-4f81-8c93-afd4962a9bdb	member	99f479dc-d57d-4b0a-8b5b-0d7447c7fdd5	2026-03-26 04:08:11.771051+00	\N	all
be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	member	0a2e82f5-1e3e-45d3-93ac-ffc22c3407b9	2026-03-26 04:16:10.479811+00	\N	all
be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	member	0a2e82f5-1e3e-45d3-93ac-ffc22c3407b9	2026-03-26 04:16:10.479811+00	\N	all
adf08b80-08f0-4272-8a5f-29c053f43acf	4363c0f0-845c-4605-8085-5eb5afef1426	member	a2f1c7b7-e158-41bb-93e9-496c3ab09112	2026-03-27 15:02:57.792638+00	\N	all
adf08b80-08f0-4272-8a5f-29c053f43acf	17e778a6-32de-43a9-87bf-dd9e7a5b8c3a	member	5c16456e-c565-4def-9031-a10a09167120	2026-03-27 15:02:57.792638+00	\N	all
\.


--
-- Data for Name: chats; Type: TABLE DATA; Schema: public; Owner: orbit
--

COPY public.chats (id, type, name, description, avatar_url, created_by, is_encrypted, max_members, created_at, updated_at) FROM stdin;
55840b43-b9ea-4d7c-abe5-2ee892f27a73	direct	\N	\N	\N	\N	f	200000	2026-03-26 04:08:11.771051+00	2026-03-26 04:08:11.771051+00
be5434cd-527d-4128-9420-3f934a5d339b	direct	\N	\N	\N	\N	f	200000	2026-03-26 04:16:10.479811+00	2026-03-26 04:16:10.479811+00
1a983ce5-170c-452c-8217-221a5a5f3bd8	group	Test Group		\N	4363c0f0-845c-4605-8085-5eb5afef1426	f	200000	2026-03-26 04:16:10.603923+00	2026-03-26 04:16:10.603923+00
adf08b80-08f0-4272-8a5f-29c053f43acf	direct	\N	\N	\N	\N	f	200000	2026-03-27 15:02:57.792638+00	2026-03-27 15:02:57.792638+00
\.


--
-- Data for Name: devices; Type: TABLE DATA; Schema: public; Owner: orbit
--

COPY public.devices (id, user_id, device_name, device_type, identity_key, push_token, push_type, last_active_at, created_at) FROM stdin;
\.


--
-- Data for Name: direct_chat_lookup; Type: TABLE DATA; Schema: public; Owner: orbit
--

COPY public.direct_chat_lookup (user1_id, user2_id, chat_id) FROM stdin;
4363c0f0-845c-4605-8085-5eb5afef1426	7bbb72d1-9635-4f81-8c93-afd4962a9bdb	55840b43-b9ea-4d7c-abe5-2ee892f27a73
4363c0f0-845c-4605-8085-5eb5afef1426	91c16381-edb3-46f8-9ca1-7071f49cbc21	be5434cd-527d-4128-9420-3f934a5d339b
17e778a6-32de-43a9-87bf-dd9e7a5b8c3a	4363c0f0-845c-4605-8085-5eb5afef1426	adf08b80-08f0-4272-8a5f-29c053f43acf
\.


--
-- Data for Name: invites; Type: TABLE DATA; Schema: public; Owner: orbit
--

COPY public.invites (id, code, created_by, email, role, max_uses, use_count, used_by, used_at, expires_at, is_active, created_at) FROM stdin;
58b0f619-66e2-443c-88e5-2272b6ed659a	91249b90	4363c0f0-845c-4605-8085-5eb5afef1426	\N	member	5	1	7bbb72d1-9635-4f81-8c93-afd4962a9bdb	2026-03-26 04:07:27.150203+00	\N	t	2026-03-26 04:05:18.096969+00
cb6fe3bc-1432-4468-8142-4c6c2c7703bb	d0e0fc66	4363c0f0-845c-4605-8085-5eb5afef1426	\N	member	1	1	91c16381-edb3-46f8-9ca1-7071f49cbc21	2026-03-26 04:14:27.388242+00	\N	t	2026-03-26 04:14:18.399698+00
7701564c-d667-4e8c-9302-267c1aa10c61	2ceaf1eb	4363c0f0-845c-4605-8085-5eb5afef1426	\N	member	1	1	17e778a6-32de-43a9-87bf-dd9e7a5b8c3a	2026-03-27 14:23:22.715545+00	\N	t	2026-03-27 14:22:47.156028+00
\.


--
-- Data for Name: messages; Type: TABLE DATA; Schema: public; Owner: orbit
--

COPY public.messages (id, chat_id, sender_id, type, content, encrypted_content, reply_to_id, is_edited, is_deleted, is_pinned, is_forwarded, forwarded_from, thread_id, expires_at, sequence_number, created_at, edited_at, entities) FROM stdin;
62f73b32-74c9-43f9-b0b3-862faf50f592	55840b43-b9ea-4d7c-abe5-2ee892f27a73	4363c0f0-845c-4605-8085-5eb5afef1426	text	Hello Orbit!	\N	\N	f	f	f	f	\N	\N	\N	1	2026-03-26 04:08:30.424551+00	\N	\N
00204835-a984-4972-a5cc-4f361f6a4113	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	57	2026-03-27 07:35:10.988926+00	\N	\N
0de21e3c-5882-4646-8422-b4d47a24072f	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	122222	\N	\N	f	f	f	f	\N	\N	\N	58	2026-03-27 07:35:14.866544+00	\N	\N
f228165c-048e-40c1-9b0b-6adea4135056	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	Hello edited!	\N	\N	t	f	f	f	\N	\N	\N	2	2026-03-26 04:16:19.347201+00	2026-03-26 04:16:51.218498+00	\N
8aea7ff9-e4eb-432f-aa41-1f358715f96c	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	Hello edited!	\N	\N	f	f	f	t	4363c0f0-845c-4605-8085-5eb5afef1426	\N	\N	4	2026-03-26 04:17:00.889587+00	\N	\N
ea2325b9-2a89-4111-9451-4784a9d3dbb5	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	\N	\N	f228165c-048e-40c1-9b0b-6adea4135056	f	t	f	f	\N	\N	\N	3	2026-03-26 04:16:19.474147+00	\N	\N
8a932843-faaf-4aff-915f-0010634c8565	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	WS test message	\N	\N	f	f	f	f	\N	\N	\N	5	2026-03-26 04:18:23.197738+00	\N	\N
adc46639-f411-45a9-ae0b-17e36b21df93	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	6	2026-03-26 11:03:14.905753+00	\N	\N
99f479dc-d57d-4b0a-8b5b-0d7447c7fdd5	55840b43-b9ea-4d7c-abe5-2ee892f27a73	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123	\N	\N	f	f	f	f	\N	\N	\N	7	2026-03-26 11:03:48.989458+00	\N	\N
8cee53db-1bd5-4e9a-bcd3-759033af5343	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	8	2026-03-26 11:05:59.194283+00	\N	\N
369d5a50-5714-4552-835e-fc59a7b4d1dd	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	9	2026-03-26 11:26:08.872666+00	\N	\N
a31826d3-00f5-41d1-a287-edca9b87d755	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	10	2026-03-26 11:27:27.077747+00	\N	\N
d75ce391-2b4f-4f50-86fa-4f8a23ccb4d0	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	2222	\N	\N	f	f	f	f	\N	\N	\N	11	2026-03-26 11:27:30.266061+00	\N	\N
afa00d6c-f3de-41ea-974a-585cc33425dc	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123	\N	\N	f	f	f	f	\N	\N	\N	12	2026-03-26 11:27:36.586648+00	\N	\N
7827e89b-399e-42b0-8b95-89078b330e74	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	13	2026-03-26 11:59:54.215963+00	\N	\N
a26594c8-30fa-4891-983d-d77b5b2c47ce	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	WS realtime test	\N	\N	f	f	f	f	\N	\N	\N	14	2026-03-26 12:05:19.189129+00	\N	\N
b41505fd-a143-48be-93bb-0331ffd47e0c	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	15	2026-03-26 12:28:13.403467+00	\N	\N
9011ba7a-efe3-4f81-b751-43922b993e26	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	22222	\N	\N	f	f	f	f	\N	\N	\N	16	2026-03-26 12:28:17.878416+00	\N	\N
315c886b-38c5-4aee-a4dc-49e0b7ae9819	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123	\N	\N	f	f	f	f	\N	\N	\N	17	2026-03-26 12:32:06.751977+00	\N	\N
fda93b21-f9b3-4d22-8c72-ca761f7a36e9	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	18	2026-03-26 12:32:19.351194+00	\N	\N
b1646888-5a51-4d5d-b3c2-c17f51a4fc26	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	222	\N	\N	f	f	f	f	\N	\N	\N	19	2026-03-26 12:32:22.861283+00	\N	\N
bd094aca-c695-499f-bb60-be8e6a1d5bbd	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	333	\N	\N	f	f	f	f	\N	\N	\N	20	2026-03-26 12:32:26.055208+00	\N	\N
3e45987b-d717-4d2f-acda-fe86e50704da	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	369d5a50-5714-4552-835e-fc59a7b4d1dd	f	f	f	f	\N	\N	\N	21	2026-03-26 12:32:55.88309+00	\N	\N
02fa0f12-8809-4691-8612-b84eeeadd76a	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	22	2026-03-26 12:35:04.16284+00	\N	\N
19a45d7b-4bfc-4fc4-8354-0622f92c293c	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	23	2026-03-26 12:35:06.959346+00	\N	\N
159a45ef-c78a-4c63-b728-1f518e4b5379	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	24	2026-03-26 12:35:16.577644+00	\N	\N
32f86a4a-7083-42a6-a761-8a46a416873f	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	25	2026-03-26 12:36:00.675473+00	\N	\N
38107f1d-39eb-4c77-bd9f-1ebb7320e05b	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	19a45d7b-4bfc-4fc4-8354-0622f92c293c	f	f	f	f	\N	\N	\N	26	2026-03-26 12:36:21.205964+00	\N	\N
b7eaa6e5-15f1-4831-83c0-34bda2cda8b2	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	27	2026-03-26 12:36:25.741677+00	\N	\N
6a9a5356-287a-444c-a8c1-439f8583bba9	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	28	2026-03-26 12:37:58.268451+00	\N	\N
43a557fe-327d-462d-93df-dfb5f2b06683	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	29	2026-03-26 12:38:03.508466+00	\N	\N
3c4fcda2-09d4-4e7d-a79b-b3814ca01932	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	30	2026-03-26 12:38:22.069849+00	\N	\N
13b85595-d488-41bf-b027-841309dcbc84	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123123	\N	32f86a4a-7083-42a6-a761-8a46a416873f	f	f	f	f	\N	\N	\N	31	2026-03-26 12:38:28.540844+00	\N	\N
b2fbecbd-8dc7-4eae-be8a-dda6ca1a906b	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	32	2026-03-26 12:39:54.37441+00	\N	\N
869ab072-8edc-4c8d-99c3-968f266ec1a1	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123	\N	\N	f	f	f	f	\N	\N	\N	33	2026-03-26 12:40:02.332505+00	\N	\N
a73f772d-4ce9-454b-a3b4-07bb6d203c07	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123213	\N	\N	f	f	f	f	\N	\N	\N	34	2026-03-26 12:40:23.239858+00	\N	\N
c95fef35-23c9-45bb-8dd4-5888a824a88f	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123	\N	6a9a5356-287a-444c-a8c1-439f8583bba9	f	f	f	f	\N	\N	\N	35	2026-03-26 12:40:37.105458+00	\N	\N
9bb699e2-c105-4a1b-be6e-3251dfc9c8da	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123123	\N	\N	f	f	f	f	\N	\N	\N	36	2026-03-26 12:41:34.793748+00	\N	\N
52a756e3-8054-4a24-8b75-26701e444ca4	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123	\N	b2fbecbd-8dc7-4eae-be8a-dda6ca1a906b	f	f	f	f	\N	\N	\N	37	2026-03-26 12:41:53.504299+00	\N	\N
01d98b96-1342-444c-94b0-624c92e828af	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123	\N	\N	f	f	f	f	\N	\N	\N	38	2026-03-26 12:45:47.03933+00	\N	\N
9ba4a232-650d-4e5a-a5ad-0db00b15a923	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123	\N	a73f772d-4ce9-454b-a3b4-07bb6d203c07	f	f	f	f	\N	\N	\N	39	2026-03-26 12:46:21.256216+00	\N	\N
469ab1d4-0a9a-4c9c-80ad-adadfca30d86	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123	\N	\N	f	f	f	f	\N	\N	\N	40	2026-03-26 12:48:20.981457+00	\N	\N
ed4a60f7-feed-4b99-8f87-07de06d0a45f	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123	\N	01d98b96-1342-444c-94b0-624c92e828af	f	f	f	f	\N	\N	\N	41	2026-03-26 12:48:50.103904+00	\N	\N
dffc7ab9-011f-4a52-8eed-af4fe80d7fb0	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123	\N	\N	f	f	f	f	\N	\N	\N	42	2026-03-26 12:51:24.083526+00	\N	\N
e7671548-1850-4dc8-b2eb-a359b5601b52	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123	\N	3c4fcda2-09d4-4e7d-a79b-b3814ca01932	f	f	f	f	\N	\N	\N	43	2026-03-26 12:52:04.375803+00	\N	\N
09f1aa52-41ce-46ce-b09c-4a50b4e575e1	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	WS realtime test message	\N	\N	f	f	f	f	\N	\N	\N	44	2026-03-26 13:28:52.60824+00	\N	\N
d360ccf1-45a8-4db0-8b86-4046f276340e	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	HELLO from API - check WS delivery	\N	\N	f	f	f	f	\N	\N	\N	45	2026-03-26 13:29:35.683929+00	\N	\N
7dba7bf5-75b4-4285-b9a0-b2f0147602de	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	WS TEST NOW	\N	\N	f	f	f	f	\N	\N	\N	46	2026-03-26 13:30:22.17082+00	\N	\N
48ee0bba-6d51-4ceb-8e66-4d39c6e5b377	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	47	2026-03-27 07:27:40.625613+00	\N	\N
41836be8-aefb-4a1f-8fdb-7a1ec2a5c908	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	48	2026-03-27 07:27:46.272978+00	\N	\N
d312c2b6-c866-45e6-a713-7dfd1041f816	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123123	\N	\N	f	f	f	f	\N	\N	\N	49	2026-03-27 07:27:47.952423+00	\N	\N
993e9de9-f2ca-4d7b-9cae-7d29994b0490	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	2222	\N	\N	f	f	f	f	\N	\N	\N	50	2026-03-27 07:27:52.052444+00	\N	\N
61c55582-96fe-4fc0-933e-28e8c983576d	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123123	\N	\N	f	f	f	f	\N	\N	\N	51	2026-03-27 07:27:56.488856+00	\N	\N
9bcab4d3-9809-4edb-8a6f-98a9be67d2ef	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	sss	\N	\N	f	f	f	f	\N	\N	\N	52	2026-03-27 07:28:01.582018+00	\N	\N
fb5e3178-b119-43f8-bb88-8f69ed549cfe	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	222	\N	\N	f	f	f	f	\N	\N	\N	53	2026-03-27 07:28:03.488125+00	\N	\N
c651047a-2573-4180-82e8-ff3a30ed9628	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	222	\N	\N	f	f	f	f	\N	\N	\N	54	2026-03-27 07:28:06.637152+00	\N	\N
a7347e66-cc64-4761-90aa-16fb54e1e0c1	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	55	2026-03-27 07:35:04.429339+00	\N	\N
4de5e01e-4b6f-481d-bd77-ec49737823ba	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	56	2026-03-27 07:35:07.357119+00	\N	\N
f68fa3f1-d686-4322-83c9-1e3f3f73a6bd	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	59	2026-03-27 07:35:35.045857+00	\N	\N
73e43e28-051c-45a6-895a-25ce5dfef342	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	60	2026-03-27 07:36:01.901041+00	\N	\N
685deb42-4f88-47e4-830a-674fa7f06557	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	61	2026-03-27 07:36:10.770785+00	\N	\N
89fa1013-d3ca-48a7-9007-53396c6d1422	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	111	\N	\N	f	f	f	f	\N	\N	\N	62	2026-03-27 07:49:39.528716+00	\N	\N
59a190cd-f33c-4b01-8304-69d4bb5ecf6d	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	63	2026-03-27 07:49:45.645786+00	\N	\N
cb87a129-69da-4239-aca4-86df26485953	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	64	2026-03-27 07:49:49.089777+00	\N	\N
6ade0b27-dfa1-4ff4-89be-f3462868ab29	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	65	2026-03-27 08:10:37.682249+00	\N	\N
fa00e60d-d0fe-4bcf-8191-c556567843bb	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	66	2026-03-27 08:10:43.314363+00	\N	\N
4727ddae-4143-47e6-96ae-498ef4cb7a24	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123123	\N	\N	f	f	f	f	\N	\N	\N	67	2026-03-27 08:10:46.535352+00	\N	\N
ef3f49b8-f222-4872-90b8-ba40d23cd43d	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	\N	\N	\N	f	t	f	f	\N	\N	\N	69	2026-03-27 08:12:09.019943+00	\N	\N
be509b11-2007-43f9-917a-71e137bdbe5f	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	\N	\N	\N	f	t	f	f	\N	\N	\N	70	2026-03-27 08:23:58.443928+00	\N	\N
a5816ca1-becf-48d0-9d3a-8e8565dd96ec	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123123	\N	\N	f	f	t	f	\N	\N	\N	68	2026-03-27 08:10:53.731569+00	\N	\N
dbf0bc0b-7047-4013-b436-9b923353b900	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	71	2026-03-27 08:25:07.943561+00	\N	\N
cf537506-3810-4507-9e05-a372338d3537	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	72	2026-03-27 08:25:11.47038+00	\N	\N
e5361912-7050-4ccf-9384-c9af131fcf18	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	73	2026-03-27 08:25:59.826908+00	\N	\N
325bc0c6-92ad-4673-a1c3-a1474081b765	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	74	2026-03-27 08:27:15.70887+00	\N	\N
054db1f7-76ed-4b24-ae7d-9f0974babee3	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	75	2026-03-27 08:27:19.375931+00	\N	\N
f26e33d7-5c24-4c45-b823-55a3c4c9a210	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	76	2026-03-27 08:27:26.284917+00	\N	\N
30546d6e-edba-4105-b5b9-e1b97167875a	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	77	2026-03-27 08:27:29.121673+00	\N	\N
b153522d-2eed-48b0-9150-1fdca407ab4a	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123123	\N	\N	f	f	f	f	\N	\N	\N	78	2026-03-27 08:27:31.535032+00	\N	\N
7bc464dd-829d-40b1-9743-ae1747a4a548	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123123123	\N	\N	f	f	f	f	\N	\N	\N	79	2026-03-27 08:27:35.740032+00	\N	\N
c19071c6-ebb0-406d-a0e7-4ed354ad49d7	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	12312313231323123	\N	\N	f	f	f	f	\N	\N	\N	80	2026-03-27 08:27:39.885775+00	\N	\N
e2940179-413f-490b-8507-5421f9d0aa2f	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123123	\N	\N	f	f	f	f	\N	\N	\N	81	2026-03-27 09:15:22.456344+00	\N	\N
151e97cb-704b-442a-80ee-4ec6e5a89c05	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123123123123123132	\N	\N	f	f	f	f	\N	\N	\N	82	2026-03-27 09:15:26.424817+00	\N	\N
0df7f7f4-4791-4e4b-83e9-1c72807440ea	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123123132	\N	\N	f	f	f	f	\N	\N	\N	83	2026-03-27 09:15:29.132001+00	\N	\N
4c263582-2e30-452e-904d-6b8586751224	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123123123	\N	\N	f	f	f	f	\N	\N	\N	84	2026-03-27 09:15:32.444264+00	\N	\N
7168fd12-435c-47b3-a472-8754c47e6504	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123123	\N	\N	f	f	f	f	\N	\N	\N	85	2026-03-27 09:15:35.216168+00	\N	\N
9d006121-8ba4-4cef-a89a-d975bd35f9c7	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123	\N	\N	f	f	f	f	\N	\N	\N	86	2026-03-27 09:15:38.194638+00	\N	\N
51b28cc8-fc1d-4b6a-9003-2084e8605f06	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123	\N	\N	f	f	f	f	\N	\N	\N	87	2026-03-27 09:15:40.776535+00	\N	\N
7955ecb0-0c3b-4d0b-8979-e4062f030673	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123123	\N	\N	f	f	f	f	\N	\N	\N	88	2026-03-27 09:15:47.703898+00	\N	\N
4cda0f8e-b4f4-4894-a3a1-33409429d584	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123123	\N	\N	f	f	f	f	\N	\N	\N	89	2026-03-27 09:15:51.221421+00	\N	\N
9306e62f-d7ce-4ac6-92e4-6d8254eb4db3	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123123123	\N	\N	f	f	f	f	\N	\N	\N	90	2026-03-27 09:15:54.164433+00	\N	\N
a1e2c931-85c1-434f-b62f-2a84177f1b6c	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123123	\N	\N	f	f	f	f	\N	\N	\N	91	2026-03-27 09:15:57.334684+00	\N	\N
1e498c87-c490-4df5-aa21-76be56a57866	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	11	\N	\N	f	f	f	f	\N	\N	\N	92	2026-03-27 09:16:03.802261+00	\N	\N
cf886fff-d5a3-46f4-a0fb-a7ef3853c667	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	1111	\N	\N	f	f	f	f	\N	\N	\N	93	2026-03-27 09:16:08.14812+00	\N	\N
efa61f22-e11a-424c-8c1f-12d228bbdccd	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	555	\N	\N	f	f	f	f	\N	\N	\N	123	2026-03-27 12:21:18.224687+00	\N	\N
dea627c2-7351-48f4-9105-2865f678c759	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	\N	\N	\N	f	t	f	f	\N	\N	\N	108	2026-03-27 12:14:21.928855+00	\N	\N
02b7c37b-8108-4808-9b54-817bb3eb26ac	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	\N	\N	\N	f	t	f	f	\N	\N	\N	94	2026-03-27 09:16:10.661063+00	\N	\N
02b9f239-f26e-4ce1-a9b5-9c05d945536a	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	111111	\N	\N	f	f	f	f	\N	\N	\N	97	2026-03-27 09:17:37.374313+00	\N	\N
83d50036-4dd6-4b85-85b0-180a07a8721b	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	333333	\N	\N	f	f	f	f	\N	\N	\N	98	2026-03-27 09:17:43.816515+00	\N	\N
3e16811a-f4fd-4b4b-af47-fd488e1833b0	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	\N	\N	\N	f	t	f	f	\N	\N	\N	96	2026-03-27 09:17:33.103573+00	\N	\N
579ddf5a-f0d0-4475-b446-b95dc2e4612c	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	111111	\N	\N	f	f	f	f	\N	\N	\N	99	2026-03-27 09:18:18.680264+00	\N	\N
e891e7a9-be75-4835-b450-d03efeee97e8	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	222222	\N	\N	f	f	f	f	\N	\N	\N	100	2026-03-27 09:18:22.880137+00	\N	\N
26c5301d-b650-4316-aa0f-73e30993e368	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	11111111	\N	\N	f	f	t	f	\N	\N	\N	95	2026-03-27 09:17:25.096139+00	\N	\N
26e3f823-69fa-4829-9b3f-80c56c5824be	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123123	\N	\N	f	f	f	f	\N	\N	\N	101	2026-03-27 12:13:52.521557+00	\N	\N
072e3461-5eb8-43e0-911d-9bd77d0b3883	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	102	2026-03-27 12:13:55.965326+00	\N	\N
07761054-2046-481e-8833-2463138413ea	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	103	2026-03-27 12:13:59.301632+00	\N	\N
c4bd50ed-35eb-490a-91e6-3c378f32b513	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	104	2026-03-27 12:14:04.866121+00	\N	\N
20eec988-6a7f-4e4f-a766-d39dc760d808	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123	\N	\N	f	f	f	f	\N	\N	\N	105	2026-03-27 12:14:09.211261+00	\N	\N
5db2e4a1-a53d-415e-ada0-688d4516e8e4	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	132	\N	\N	f	f	f	f	\N	\N	\N	106	2026-03-27 12:14:15.801403+00	\N	\N
a23fc505-87d8-485e-93f8-2701e667db51	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	107	2026-03-27 12:14:19.703542+00	\N	\N
411bdce3-4839-4cce-8f5d-254840f9ef7f	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123123	\N	\N	f	f	f	f	\N	\N	\N	109	2026-03-27 12:14:25.307047+00	\N	\N
5eafd55f-e155-45b8-9fd0-5571586c4c76	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123123123	\N	\N	f	f	f	f	\N	\N	\N	110	2026-03-27 12:14:30.875812+00	\N	\N
6292a9f6-1be8-48d2-b092-0e9a1f5e2f68	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	123123	\N	\N	f	f	f	f	\N	\N	\N	111	2026-03-27 12:14:52.525658+00	\N	\N
934eff6d-846f-4ccb-8bde-bf4e1d15f6f5	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	23232323	\N	\N	f	f	f	f	\N	\N	\N	112	2026-03-27 12:14:59.756762+00	\N	\N
86e40204-5620-4b60-9d0f-fc6076b36150	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	22	\N	\N	f	f	f	f	\N	\N	\N	113	2026-03-27 12:15:11.454251+00	\N	\N
e6092a6b-d732-4da2-88d0-483f72ad367d	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	111	\N	\N	f	f	f	f	\N	\N	\N	114	2026-03-27 12:15:56.175404+00	\N	\N
6f47f9c7-39f3-4d88-b5bc-3470b86d56b4	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	1111	\N	\N	f	f	f	f	\N	\N	\N	115	2026-03-27 12:16:37.151349+00	\N	\N
6eb6768e-e3ad-4eb8-a8c5-4d13fa90c7ce	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	222	\N	\N	f	f	f	f	\N	\N	\N	116	2026-03-27 12:17:19.208719+00	\N	\N
15382196-abf1-42fa-8d5b-5d3152f9e63e	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	333	\N	\N	f	f	f	f	\N	\N	\N	117	2026-03-27 12:18:21.991715+00	\N	\N
4c49a331-2433-4043-bc78-67db066cbe9a	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	444	\N	\N	f	f	f	f	\N	\N	\N	118	2026-03-27 12:18:30.120278+00	\N	\N
48a61423-ddfe-43a7-a8a8-c5f1cfd3fecf	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	555	\N	\N	f	f	f	f	\N	\N	\N	119	2026-03-27 12:20:42.993845+00	\N	\N
bf668799-061d-4578-bbc0-a78882123835	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	5555	\N	\N	f	f	f	f	\N	\N	\N	120	2026-03-27 12:20:47.682285+00	\N	\N
201ff4a9-6f25-4169-88f2-d387e81d0fbb	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	5555	\N	\N	f	f	f	f	\N	\N	\N	121	2026-03-27 12:21:08.454457+00	\N	\N
74613a77-78c7-4e3b-9cb3-c705a9c5d970	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	555	\N	\N	f	f	f	f	\N	\N	\N	122	2026-03-27 12:21:13.09548+00	\N	\N
ffe78941-6e2e-42c0-ad5a-34b26205b407	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	124	2026-03-27 12:39:19.969249+00	\N	\N
c7124868-b285-40ce-9100-80c5c64285cb	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	125	2026-03-27 12:39:24.195419+00	\N	\N
2a70bec6-ae31-46ff-ae16-cbd3818e8095	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	126	2026-03-27 12:39:27.392807+00	\N	\N
5f38c78f-e12b-4a1b-9db6-a6019daabfd8	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	127	2026-03-27 12:40:27.892702+00	\N	\N
16cfc354-0730-473d-8c02-9048470a065f	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	128	2026-03-27 12:40:32.176336+00	\N	\N
af4a378d-0e47-4364-8077-4ca6a5c69f59	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	129	2026-03-27 12:40:34.368272+00	\N	\N
cec06693-8c97-485c-8180-e648ace12aca	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	222222	\N	\N	f	f	f	f	\N	\N	\N	130	2026-03-27 12:40:43.38139+00	\N	\N
31a8d945-0111-4a18-9afe-395213295620	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	2222222	\N	\N	f	f	f	f	\N	\N	\N	131	2026-03-27 12:40:48.379349+00	\N	\N
79f99191-f37a-47b3-8494-e34fe077090a	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	132	2026-03-27 12:42:23.024133+00	\N	\N
683ac950-b600-40ec-b6ca-ff85c97a348b	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	22222	\N	\N	f	f	f	f	\N	\N	\N	133	2026-03-27 12:42:27.95743+00	\N	\N
a13d795d-8e94-4a66-af47-2c05babc5450	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	проверка чата	\N	\N	f	f	f	f	\N	\N	\N	134	2026-03-27 12:43:11.500433+00	\N	\N
6a0379d4-a086-4acc-b486-e5f54a011515	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	проверка чата 2	\N	\N	f	f	f	f	\N	\N	\N	135	2026-03-27 12:43:19.076827+00	\N	\N
ce87c11d-fb8e-45d2-b95f-865b727106cf	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	проверка чата 3	\N	\N	f	f	f	f	\N	\N	\N	136	2026-03-27 12:47:26.185565+00	\N	\N
a3127a7a-7584-483f-bbe5-95ea9c3c16db	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	проверка чата 4	\N	\N	f	f	f	f	\N	\N	\N	137	2026-03-27 12:47:33.402305+00	\N	\N
db7330d8-29a9-4b80-abc1-254a9f3e3edd	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	проверка чата 5	\N	\N	f	f	f	f	\N	\N	\N	138	2026-03-27 12:47:41.272781+00	\N	\N
7388693e-372d-4ec6-b94d-9562ea7ac1ed	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	проверка чата 6	\N	\N	f	f	f	f	\N	\N	\N	139	2026-03-27 12:47:48.534418+00	\N	\N
8d6ff57b-28e7-48e1-99f3-d2d3d86c3d11	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	проверка чата 7	\N	\N	f	f	f	f	\N	\N	\N	140	2026-03-27 12:47:56.82665+00	\N	\N
1365e3f3-f89a-404d-b403-0b6829026287	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	проверка чата 8	\N	\N	f	f	f	f	\N	\N	\N	141	2026-03-27 12:51:29.428557+00	\N	\N
35ffa051-04f5-4600-b662-1af9632b13ea	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	проверка чата 9	\N	\N	f	f	f	f	\N	\N	\N	142	2026-03-27 12:51:36.943473+00	\N	\N
c43591c5-ebd5-421a-8d07-38f3f8d69d61	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	проверка чата 10	\N	\N	f	f	f	f	\N	\N	\N	143	2026-03-27 12:51:43.438776+00	\N	\N
5ce7cad2-f99d-43a3-a02e-4b3f7bde9bdf	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	проверка чата 11	\N	\N	f	f	f	f	\N	\N	\N	144	2026-03-27 12:51:47.079275+00	\N	\N
0fde1f05-dd70-4a88-b910-875d862f0658	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	111	\N	\N	f	f	f	f	\N	\N	\N	145	2026-03-27 12:52:24.530914+00	\N	\N
a17efb58-b2b1-49ad-b04e-cef2e8947df4	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	Второе окно пишу сообщение	\N	\N	f	f	f	f	\N	\N	\N	148	2026-03-27 12:57:59.985335+00	\N	\N
71e46f95-70aa-4f4c-a852-84905aeaf9d6	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	Первое окно пишу второе сообщение	\N	\N	f	f	f	f	\N	\N	\N	149	2026-03-27 12:58:06.618636+00	\N	\N
2144e134-8ea1-416c-94e1-b3b776f46d8c	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	Второе окно пишу первое сообщение	\N	\N	f	f	f	f	\N	\N	\N	150	2026-03-27 12:58:12.454066+00	\N	\N
99a4ee93-77c8-4e5c-98d4-6bfaf9c23c58	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	153	2026-03-27 13:08:33.800068+00	\N	\N
6bd5133d-5904-4f91-a584-56259a03e705	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	154	2026-03-27 13:08:38.116967+00	\N	\N
7c3983e5-2151-45cb-b96c-660ee5329a20	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	155	2026-03-27 13:10:24.06014+00	\N	\N
6406f847-8025-4c90-a84c-d861bd32c043	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123123	\N	\N	f	f	f	f	\N	\N	\N	156	2026-03-27 13:10:30.44329+00	\N	\N
c0872eb9-2f8b-4603-8cf8-6c2d52061649	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	1111	\N	\N	f	f	f	f	\N	\N	\N	146	2026-03-27 12:52:41.895917+00	\N	\N
637d6d1a-18ee-4b03-9133-c2945bab339e	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	Первое окно пишу сообщение	\N	\N	f	f	f	f	\N	\N	\N	147	2026-03-27 12:57:43.456355+00	\N	\N
662fed93-87f5-4ca1-884f-f4623d19b552	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	привет	\N	\N	f	f	f	f	\N	\N	\N	151	2026-03-27 13:05:40.503545+00	\N	\N
f0703aae-df94-4cca-a744-fb543b54ad2e	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	привет 2	\N	\N	f	f	f	f	\N	\N	\N	152	2026-03-27 13:05:46.554943+00	\N	\N
dcf4e56f-d9ba-4684-8b15-84eba1108cf2	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123123312	\N	\N	f	f	f	f	\N	\N	\N	157	2026-03-27 13:10:55.243228+00	\N	\N
9ce5d2c5-42c1-4dc3-8a18-cfc0d2110d76	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	2222	\N	\N	f	f	f	f	\N	\N	\N	158	2026-03-27 13:26:38.890553+00	\N	\N
536cc2c6-2009-40d0-ab4f-f51f2a1a7390	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	33333	\N	\N	f	f	f	f	\N	\N	\N	159	2026-03-27 13:26:42.925043+00	\N	\N
692a74a1-d16e-49ed-a814-35548db07ec8	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	4444	\N	\N	f	f	f	f	\N	\N	\N	160	2026-03-27 13:26:45.695794+00	\N	\N
68b1a70e-0fb0-45e6-8597-23bcc6821356	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	222	\N	\N	f	f	f	f	\N	\N	\N	161	2026-03-27 13:26:48.058561+00	\N	\N
169ae47f-78c3-490c-a609-02a99bf77341	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	33	\N	\N	f	f	f	f	\N	\N	\N	162	2026-03-27 13:26:52.338677+00	\N	\N
62e33250-f59b-4f10-bc03-8f333663be73	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	333	\N	\N	f	f	f	f	\N	\N	\N	163	2026-03-27 13:26:57.350828+00	\N	\N
e06345f8-8c37-4b64-b6f5-e2b983f9c288	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	4444	\N	\N	f	f	f	f	\N	\N	\N	164	2026-03-27 13:27:01.771694+00	\N	\N
6912a1b2-20ee-4a6c-ab27-84874e6b0791	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	2222	\N	\N	f	f	f	f	\N	\N	\N	165	2026-03-27 13:27:57.055222+00	\N	\N
91a1733e-bf78-4bcf-a1dd-b877f930c7ed	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	3333	\N	\N	f	f	f	f	\N	\N	\N	166	2026-03-27 13:28:04.100115+00	\N	\N
ec21ad34-97dd-466b-a05b-9bf49c75e7fc	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	22222	\N	\N	f	f	f	f	\N	\N	\N	167	2026-03-27 13:28:06.627946+00	\N	\N
b1ea5a1f-148e-4ee8-b409-986e4cb831a2	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	чек	\N	\N	f	f	f	f	\N	\N	\N	168	2026-03-27 13:30:37.59677+00	\N	\N
12cd067b-86fd-49cb-b250-c748ebb3b065	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	чек 2	\N	\N	f	f	f	f	\N	\N	\N	169	2026-03-27 13:30:52.364271+00	\N	\N
84bc1a5d-d5b5-4c82-8814-0ed405ee5417	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	123	\N	\N	f	f	f	f	\N	\N	\N	170	2026-03-27 13:52:55.253085+00	\N	\N
be579bf7-6582-4267-b0b4-52037f7bcf49	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	22222	\N	\N	f	f	f	f	\N	\N	\N	171	2026-03-27 13:52:57.923806+00	\N	\N
e74d66f6-c2e0-4d75-b78a-cbacac2d7e64	be5434cd-527d-4128-9420-3f934a5d339b	91c16381-edb3-46f8-9ca1-7071f49cbc21	text	3333333	\N	\N	f	f	f	f	\N	\N	\N	172	2026-03-27 13:53:03.030998+00	\N	\N
e3d6b766-b0d1-4a98-a4cd-0213c8c95558	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	2222	\N	\N	f	f	f	f	\N	\N	\N	174	2026-03-27 13:58:26.268308+00	\N	\N
b32dbeb6-8f92-4dd1-b844-363bb9938b29	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	332323	\N	\N	f	f	f	f	\N	\N	\N	175	2026-03-27 14:02:55.359842+00	\N	\N
1542b904-fb1c-4d07-9451-c2d096f827e0	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	22	\N	\N	f	f	f	f	\N	\N	\N	176	2026-03-27 14:06:56.143148+00	\N	\N
0a2e82f5-1e3e-45d3-93ac-ffc22c3407b9	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	1111	\N	\N	f	f	f	f	\N	\N	\N	177	2026-03-27 14:06:57.908581+00	\N	\N
ab851063-7b97-4409-9776-67601717f2a8	be5434cd-527d-4128-9420-3f934a5d339b	4363c0f0-845c-4605-8085-5eb5afef1426	text	\N	\N	\N	f	t	f	f	\N	\N	\N	173	2026-03-27 13:58:15.343309+00	\N	\N
6625a550-2020-4c71-912b-20526981b81a	adf08b80-08f0-4272-8a5f-29c053f43acf	17e778a6-32de-43a9-87bf-dd9e7a5b8c3a	text	1222	\N	\N	f	f	f	f	\N	\N	\N	178	2026-03-27 15:08:24.037587+00	\N	\N
a2f1c7b7-e158-41bb-93e9-496c3ab09112	adf08b80-08f0-4272-8a5f-29c053f43acf	17e778a6-32de-43a9-87bf-dd9e7a5b8c3a	text	2222	\N	\N	f	f	f	f	\N	\N	\N	179	2026-03-27 15:09:02.733515+00	\N	\N
5c16456e-c565-4def-9031-a10a09167120	adf08b80-08f0-4272-8a5f-29c053f43acf	17e778a6-32de-43a9-87bf-dd9e7a5b8c3a	text	333	\N	\N	f	f	f	f	\N	\N	\N	180	2026-03-27 15:09:32.374021+00	\N	\N
\.


--
-- Data for Name: sessions; Type: TABLE DATA; Schema: public; Owner: orbit
--

COPY public.sessions (id, user_id, device_id, token_hash, ip_address, user_agent, expires_at, created_at) FROM stdin;
b903f669-54c1-4ceb-8fc3-95e56d2ce894	4363c0f0-845c-4605-8085-5eb5afef1426	\N	2ea98f6f566845920870cafcfbd9f345d05e7307cd50d66988e35d307b587dbc	172.18.0.7	curl/8.17.0	2026-04-25 04:05:06.737977+00	2026-03-26 04:05:06.738698+00
d85ce4d7-fb5f-410c-b778-78c264483e92	4363c0f0-845c-4605-8085-5eb5afef1426	\N	59b2ba979370a9afc5c8ff8a75e221babe2fa6f324f7669e848dc7e23340459f	172.18.0.7	curl/8.17.0	2026-04-25 04:05:18.050917+00	2026-03-26 04:05:18.051717+00
442e733d-d923-46a7-a661-a0fb16e24be8	4363c0f0-845c-4605-8085-5eb5afef1426	\N	7ac46778660055d073700c4bae039bb256b6c8e8755417ac317f6e9e3b28536c	172.18.0.7	curl/8.17.0	2026-04-25 04:07:37.725466+00	2026-03-26 04:07:37.726084+00
101b3884-5fe0-4b44-aa82-15b2df8abac8	4363c0f0-845c-4605-8085-5eb5afef1426	\N	00532d27f247eeada76d635c604372d4802391e12628517b457ac8cc3c83a0ee	172.18.0.7	curl/8.17.0	2026-04-25 04:08:11.722467+00	2026-03-26 04:08:11.723027+00
f1728bc5-ef02-4407-a97e-f62982548eca	4363c0f0-845c-4605-8085-5eb5afef1426	\N	0952ed1769d01bd01ee97615745b6a9cd53a5704737e4dc8d3495155fecf22b1	172.18.0.1	curl/8.17.0	2026-04-25 04:08:30.371511+00	2026-03-26 04:08:30.372179+00
ad8519fa-8b21-413a-9237-2c72d7c3f12a	4363c0f0-845c-4605-8085-5eb5afef1426	\N	3cb699b79a5d0b4fa347d3c412d15c1a5ba313b0cf3825b96416092ec89e1956	172.18.0.1	curl/8.17.0	2026-04-25 04:08:47.421691+00	2026-03-26 04:08:47.422327+00
f4821eb1-e2b2-4413-b544-05ff81a2bdb4	4363c0f0-845c-4605-8085-5eb5afef1426	\N	0d36d55365d93a5b17fdb707a674333e6caff9646de0bd42560d6ce5c95be6af	172.18.0.7	curl/8.17.0	2026-04-25 04:12:36.048185+00	2026-03-26 04:12:36.048783+00
46bea856-ee99-4895-a854-9c50f2f4e523	4363c0f0-845c-4605-8085-5eb5afef1426	\N	523bc8e4c42dcdc910fb7d6c6950caac753a9bf2fadb931ced5c2583db3c6b75	172.18.0.7	curl/8.17.0	2026-04-25 04:14:18.232179+00	2026-03-26 04:14:18.232796+00
7e7e8d6c-b5e6-42a2-8132-d4d682780eb0	4363c0f0-845c-4605-8085-5eb5afef1426	\N	2f6f0b4ca849b7471fb477d2f8391a9d5e67cd9286113dd5b9b2cf3e5907bf43	172.18.0.7	curl/8.17.0	2026-04-25 04:16:01.191573+00	2026-03-26 04:16:01.192098+00
f7da2e67-f6db-415d-a532-7181a83c494e	91c16381-edb3-46f8-9ca1-7071f49cbc21	\N	f4d0a4a53ea67d839f0ab70f649a23bc6e83bae0b87c72ef359dfd6e45c2dc2d	172.18.0.7	curl/8.17.0	2026-04-25 04:16:01.437402+00	2026-03-26 04:16:01.437931+00
87a542d9-b21a-487d-b1bb-63d4fc583472	4363c0f0-845c-4605-8085-5eb5afef1426	\N	cd093c69b74f5728365f2909fc2e914b7900fde5e44844dfb6e3580a058038b9	172.18.0.7	curl/8.17.0	2026-04-25 04:18:47.264508+00	2026-03-26 04:18:47.26499+00
2eccdb09-a286-491d-9340-86500763e047	4363c0f0-845c-4605-8085-5eb5afef1426	\N	7ffda7af8c44d2d065d3a30fea18d92d4d42e6417dfa84d6b478d14fd4885bf2	172.18.0.8	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-26 14:40:33.165469+00	2026-03-27 14:40:33.165774+00
47f60176-1c21-4ffe-9ca4-c36178fa0f30	4363c0f0-845c-4605-8085-5eb5afef1426	\N	b34a0eb29d5426ed858ddaa9a5cdac6fe8a782a3e97c9db541e398ba974d1293	172.18.0.7	curl/8.17.0	2026-04-25 04:18:47.594413+00	2026-03-26 04:18:47.594695+00
a7991921-b963-4607-aa63-3d4b9077bf62	4363c0f0-845c-4605-8085-5eb5afef1426	\N	39f0b4aca087c88eef992ed1af8ff06637bd674ab8ed180da8649683268acd61	172.18.0.7	curl/8.17.0	2026-04-25 04:25:10.458691+00	2026-03-26 04:25:10.45941+00
8179bd20-cc44-457b-a61e-5cea69ac2d06	91c16381-edb3-46f8-9ca1-7071f49cbc21	\N	b3a0002cb664e3d59c85628b25aa4bbf6e973bd6880d2c605404a8dc5aeb0993	172.18.0.7	curl/8.17.0	2026-04-25 04:25:10.84964+00	2026-03-26 04:25:10.850181+00
7108b063-07a2-437c-9c50-91576671d6e3	91c16381-edb3-46f8-9ca1-7071f49cbc21	\N	d640676c85f4f6c27d25bd88d914649865589bf07c4b18c1ae823d466bf8a17c	172.18.0.7	curl/8.17.0	2026-04-25 04:26:37.078146+00	2026-03-26 04:26:37.078773+00
ef88feff-5f96-4776-9cfc-f8e2c6d1d5c4	4363c0f0-845c-4605-8085-5eb5afef1426	\N	77466950ad8b60c19e630181abb377ab950ee8fe757acd331a287f5546783a83	172.18.0.7	curl/8.17.0	2026-04-25 04:39:21.027179+00	2026-03-26 04:39:21.028794+00
428e9d47-067c-4275-852f-99d31b59f289	4363c0f0-845c-4605-8085-5eb5afef1426	\N	64b1ae008ba8042c5b2c1ca2d2c44b4c77d762ef0ee748eba00c0bc29ce252a2	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 04:42:37.796607+00	2026-03-26 04:42:37.797388+00
c61aab34-e0ef-4ecc-bce5-2613e53df4a9	4363c0f0-845c-4605-8085-5eb5afef1426	\N	43232fbc530f03f14fc5e07e31ccaa31cef26d636dcdf7b7cf02c874d8df0ca6	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 04:42:54.563249+00	2026-03-26 04:42:54.563861+00
d9230eb3-a3fc-4acb-9921-883dfcb64cb1	4363c0f0-845c-4605-8085-5eb5afef1426	\N	3e9544ae02269b5c78269c59d979d0f13ed53e4f71c1944d97011d37e75efa59	172.18.0.7	curl/8.17.0	2026-04-25 04:44:33.922563+00	2026-03-26 04:44:33.923185+00
bd367cd8-3adf-436c-9b48-9f6d99f40828	4363c0f0-845c-4605-8085-5eb5afef1426	\N	a0fd64f73c5d6f8b6da6147d488878b8799bb088824eb70f9a9ca644b7d0c4d6	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 04:48:56.359023+00	2026-03-26 04:48:56.361393+00
d0be9f7c-b60a-405e-b7ba-ae148857c9e3	4363c0f0-845c-4605-8085-5eb5afef1426	\N	134742f88f6b2bcbf7c16d6e6ce82cc2bc78e6ec410dbd2d8a7e6cf88927a4aa	172.18.0.7	curl/8.17.0	2026-04-25 04:49:47.035723+00	2026-03-26 04:49:47.036533+00
46355246-d1e7-4ad2-809e-e57056743385	4363c0f0-845c-4605-8085-5eb5afef1426	\N	be56327b487629522d016d204c4c4628e290067117bf3b32498bc5dadb6429d6	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 04:52:49.338267+00	2026-03-26 04:52:49.339015+00
832e2d19-abe0-4cb5-ac6a-7db7549e1550	4363c0f0-845c-4605-8085-5eb5afef1426	\N	c9c9e7eb7601721b0218dbbcd21dc4a8913099913bca116837b8b61d9915bc6a	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 04:53:47.085546+00	2026-03-26 04:53:47.086345+00
9ef3879c-5696-4752-86e1-8dc5b19a5741	4363c0f0-845c-4605-8085-5eb5afef1426	\N	dbf4887e3f0419597c71121d106160c8b316ffc5bcd486afee71e6e6b672cbef	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 04:54:42.777386+00	2026-03-26 04:54:42.778201+00
84858716-a66f-4090-9c04-c5b78d5f455e	4363c0f0-845c-4605-8085-5eb5afef1426	\N	d3f0b676702bd0979158adc3368142d1f3a13dae74fd1062e989dd958701fec8	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 04:55:29.391033+00	2026-03-26 04:55:29.391761+00
13c34e11-12d1-4b6b-9361-e1bfcdc8b3ae	4363c0f0-845c-4605-8085-5eb5afef1426	\N	7b84aefb428b510a2e85c5bfa68b5b1fd14d5c6722686edbb71f3db4fc9c2600	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 04:57:23.37445+00	2026-03-26 04:57:23.37519+00
5b2aeac3-6aca-4838-9311-acfc77de2bfe	4363c0f0-845c-4605-8085-5eb5afef1426	\N	bfd20362487515152cc44070422e5a255017e6b0a51470e11d04f70c4986adb3	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 04:57:56.157511+00	2026-03-26 04:57:56.158208+00
377691d5-bfb8-453a-960a-6e67c2d0abbf	4363c0f0-845c-4605-8085-5eb5afef1426	\N	561b778cb644be37feeb495d55db07af83ba5684f2b11a5576fd1ea2ebe2fdc0	172.18.0.7	curl/8.17.0	2026-04-25 04:58:58.635083+00	2026-03-26 04:58:58.635721+00
fbff1ce8-7336-498f-91aa-e306a8301c0d	4363c0f0-845c-4605-8085-5eb5afef1426	\N	795c09c52ff77a59ff4b1b2aee09fa56399fd01ee69135024df32642956f6238	172.18.0.7	curl/8.17.0	2026-04-25 05:00:18.432735+00	2026-03-26 05:00:18.434336+00
53006fd4-d611-4896-9362-9bfb37db8cc5	4363c0f0-845c-4605-8085-5eb5afef1426	\N	457d98fae6e560f937384073c6c153910b19bf51efa27bf7e9f7c61a4d86c1ff	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 05:03:26.037085+00	2026-03-26 05:03:26.037952+00
67502c65-43c8-4931-89fd-be09a457f4cc	4363c0f0-845c-4605-8085-5eb5afef1426	\N	4ff70d5f417c30df0906133b6ab1978ff26b68b10426acead49fef6f3620be90	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 05:06:58.382234+00	2026-03-26 05:06:58.38287+00
95997467-5a4d-4e85-a14f-17641ed31375	4363c0f0-845c-4605-8085-5eb5afef1426	\N	f21f5522107342b43bcd8ce05bb6021531b6e748242d971086319e34cb3d1fdd	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 05:16:53.882415+00	2026-03-26 05:16:53.883165+00
7d109506-03c1-4444-be94-d43d6e286a9a	4363c0f0-845c-4605-8085-5eb5afef1426	\N	06fcb691750afa9ffc28937f5cc24b2a73a780340db8231362e665019e17986d	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 05:32:43.279059+00	2026-03-26 05:32:43.281003+00
bd739c04-3c95-4c86-82ae-65a0d2d7006a	4363c0f0-845c-4605-8085-5eb5afef1426	\N	a57f22e240c3a6651693390f3d949cd4e400cbf67d97d7c74fae95c1f8843cec	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 05:36:15.229946+00	2026-03-26 05:36:15.231142+00
c14b03b6-b13c-464e-9234-8d9e7d0e33f9	4363c0f0-845c-4605-8085-5eb5afef1426	\N	d40b0f022ab71842aeb0dcb4ac436ff29029590a9d0f4cedea01208777878855	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 05:37:44.201204+00	2026-03-26 05:37:44.201841+00
ab065bba-98e5-4efb-b69d-1cbe6d1028bd	4363c0f0-845c-4605-8085-5eb5afef1426	\N	e8a4256c1daa570f23d0dad633da8c657546ae567a13449a73c061def65ad97e	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 05:44:34.370351+00	2026-03-26 05:44:34.372553+00
58a08f6a-7006-447d-8b46-f6aa3f336cf3	4363c0f0-845c-4605-8085-5eb5afef1426	\N	64d80a427b4d1fa5413dc28dee845360e9ec834f1f9ca9e96e75329bc7755cee	172.18.0.7	curl/8.17.0	2026-04-25 05:45:02.96877+00	2026-03-26 05:45:02.969399+00
75b7507b-547f-40f8-bc2e-25cdca3e30bf	4363c0f0-845c-4605-8085-5eb5afef1426	\N	457a2ca9e8fd29e9795a50fff602bb87584f53fb40673b7e6cf835c5988a5438	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 05:53:54.600294+00	2026-03-26 05:53:54.601846+00
495044cb-16de-4403-8344-c8550d8bb1a2	4363c0f0-845c-4605-8085-5eb5afef1426	\N	25ecddcd6591d1beba6837cd1d93b31b7f59ca6fe281f259ed1fa41c8b5eb8d8	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 05:54:47.881165+00	2026-03-26 05:54:47.881783+00
df37c077-37de-4973-ace6-621771088937	4363c0f0-845c-4605-8085-5eb5afef1426	\N	ac2d9aa765456b8d54363a2197ddfb3ef09166085d9f21463a5997de18400a41	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 05:56:01.74222+00	2026-03-26 05:56:01.742806+00
324eb385-2749-4b19-b2f8-eb97774ae8a4	4363c0f0-845c-4605-8085-5eb5afef1426	\N	b5145ad823db25e6a60a43517d99296049233ce7dd38d72d4017c58fd0dfc594	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 06:16:28.25568+00	2026-03-26 06:16:28.257459+00
9836e91e-cddd-49a2-9616-3e895fa20204	4363c0f0-845c-4605-8085-5eb5afef1426	\N	263c080341cb505a89e4302e68b3a636edcf67c0242cbba1ff429781462edc5f	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 06:20:41.625934+00	2026-03-26 06:20:41.627031+00
396a9ea4-5fa4-4eaa-bce8-fb90361863b5	4363c0f0-845c-4605-8085-5eb5afef1426	\N	88e73cd3d884ee6034d0c75bd45a8c70f67fc07586c9921a35ef5d43bc285185	172.18.0.7	curl/8.17.0	2026-04-25 06:22:07.212885+00	2026-03-26 06:22:07.214206+00
0c28cdc7-59a7-49e4-83c4-243e25abe3bc	4363c0f0-845c-4605-8085-5eb5afef1426	\N	413b9164f0d6132659abb5d7b82e231fe65993b3e984735db07c8ada550dea88	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 06:25:51.754539+00	2026-03-26 06:25:51.75597+00
d20d2943-d8ad-4900-b104-71a8fe784235	4363c0f0-845c-4605-8085-5eb5afef1426	\N	99288b5a112392126332bed027090c5a7cf82f9915bbc37523a158da2f0675c2	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 06:30:43.664999+00	2026-03-26 06:30:43.666446+00
252432ed-2a2b-4ef6-8cb6-4500ed115da4	4363c0f0-845c-4605-8085-5eb5afef1426	\N	ca0ac31871f057ad198c3af7ea1e511ca711760f964f523e2fd8ddbc11691e01	172.18.0.7	curl/8.17.0	2026-04-25 06:31:51.565782+00	2026-03-26 06:31:51.566521+00
64df76b5-ed4f-4608-985a-a5db21981279	4363c0f0-845c-4605-8085-5eb5afef1426	\N	f476edfc73134f2a037dc3ff87e272ba02aa2d3e513ee64f9796d87f885a8107	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 06:34:35.049604+00	2026-03-26 06:34:35.050607+00
a771223d-ab82-44e4-844b-36dca6468c84	4363c0f0-845c-4605-8085-5eb5afef1426	\N	3ca0c4bc38bd35eaf68f67d6682e49c4d69fe3bfd74f4aef06950baeee13f54d	172.18.0.1	curl/8.17.0	2026-04-25 06:42:08.500567+00	2026-03-26 06:42:08.50143+00
0315fc65-5682-4ff3-b960-f3b0caebf56f	4363c0f0-845c-4605-8085-5eb5afef1426	\N	c3fdc3039e9cd052a66549585bd21f9f9d0cc75a1b6c68b8685628db589369bf	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 07:11:51.438319+00	2026-03-26 07:11:51.439851+00
81bb9847-13c7-4844-bd99-486373e7d540	4363c0f0-845c-4605-8085-5eb5afef1426	\N	2428f1f840e3e6bdd56ac732daedd1255e92764897600c95ed58c7b140e7ff50	172.18.0.7	curl/8.17.0	2026-04-25 07:12:08.327337+00	2026-03-26 07:12:08.327975+00
ea2cb5a9-a7ee-4390-bb81-9448a365fb2b	4363c0f0-845c-4605-8085-5eb5afef1426	\N	c8657e654d49aea52274801c6a1d29544e2a8b19348253a9f5928e7d4b4a68e1	172.18.0.7	curl/8.17.0	2026-04-25 13:28:31.133925+00	2026-03-26 13:28:31.136072+00
d245c349-d3e2-488f-92c5-1506d9486874	4363c0f0-845c-4605-8085-5eb5afef1426	\N	a0608353c41ffa65384406076c5b5e33e327bede6d8497cd8ae57d66977e493b	172.18.0.7	curl/8.17.0	2026-04-25 13:28:40.671608+00	2026-03-26 13:28:40.672272+00
960f44f8-e8cd-4be4-942e-c070cfd529a8	4363c0f0-845c-4605-8085-5eb5afef1426	\N	c18d3c3247a81d11092d4d1c0cdf429dc8c3319e0fd94b4d8392ea04173946c9	172.18.0.7	curl/8.17.0	2026-04-25 07:23:39.785807+00	2026-03-26 07:23:39.786314+00
c771fc80-41ca-4c22-9475-67c75e1d324c	4363c0f0-845c-4605-8085-5eb5afef1426	\N	0d446eb39fd5631050f6a7f0d37cc3cf810364237c438f9b4149503fe6770e9d	172.18.0.7	curl/8.17.0	2026-04-25 07:23:53.333161+00	2026-03-26 07:23:53.33376+00
279238a9-35cb-4905-9b21-cfb0c917561c	4363c0f0-845c-4605-8085-5eb5afef1426	\N	58e87d41a529c7894ad6f8cdeda88d0940790fd6581a082f61b4c2ac12eb9b0b	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 07:55:17.231552+00	2026-03-26 07:55:17.232124+00
86f496f4-d230-47e3-97c4-ae7bea749a41	4363c0f0-845c-4605-8085-5eb5afef1426	\N	b4e6d08fcb0a3a84940b08704be199bf5717821b06cdd298c73828b6ae9b3016	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 07:57:05.702078+00	2026-03-26 07:57:05.704173+00
89a23760-7ccc-484c-8a84-0a454cc4bbb4	4363c0f0-845c-4605-8085-5eb5afef1426	\N	1065657301c5983efeb3dcb82d10f8436fc9b1c817b9f82cf199278349a7d31b	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 09:05:51.233848+00	2026-03-26 09:05:51.234474+00
03732922-c655-4a95-b7a1-ff9c7cad8cc0	4363c0f0-845c-4605-8085-5eb5afef1426	\N	af4277491079d9094231ed0d4ca02808fff2ecbaba6577e802c6f4babeb173e6	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 YaBrowser/26.3.0.0 Safari/537.36	2026-04-25 10:12:36.767879+00	2026-03-26 10:12:36.768181+00
3690d6ba-7800-484e-b054-2663bb7c0c7c	4363c0f0-845c-4605-8085-5eb5afef1426	\N	fb5804811fe19eb42ca55a14cc4c7164d44da0b6b91e4eb63b546bf5d24ce358	172.18.0.7	curl/8.17.0	2026-04-26 08:12:08.744002+00	2026-03-27 08:12:08.744619+00
9dfba13b-0d5a-42a7-8036-07be0f3d2c82	4363c0f0-845c-4605-8085-5eb5afef1426	\N	6d501218b7f311e420839d94a2b88fadd0d08e13c63a021adb58f281f53bc268	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 10:13:39.252548+00	2026-03-26 10:13:39.252926+00
a0426682-38dd-4d86-818b-252d3ec9386f	4363c0f0-845c-4605-8085-5eb5afef1426	\N	8a38e96e3fd1c9781ea1d3792437f43645ecc7f422f9459c0433abdd59440244	172.18.0.7	curl/8.17.0	2026-04-26 08:12:20.154178+00	2026-03-27 08:12:20.154726+00
216c8875-c5aa-499e-b154-0095ba7863a2	4363c0f0-845c-4605-8085-5eb5afef1426	\N	10f29f3a1b0b8f8dbb34fc7ce67c072397c208b3470ab9bf868d9dce4aa131e2	172.18.0.7	curl/8.17.0	2026-04-26 08:12:29.576457+00	2026-03-27 08:12:29.577024+00
40ab008a-6a3d-4831-b41c-262c8f527e7e	4363c0f0-845c-4605-8085-5eb5afef1426	\N	7ebc64e8c369b64a61aa08a231c4a239313e5e80614dfc78c0e8d641afb1a8d1	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/144.0.0.0 YaBrowser/26.3.0.0 Safari/537.36	2026-04-26 08:22:58.542448+00	2026-03-27 08:22:58.542878+00
26b461f5-13f4-42ea-a252-4ccc54d4d776	4363c0f0-845c-4605-8085-5eb5afef1426	\N	69a1fde8e0676a21e2b5cb015663327db07e694c330ea29ed4004a57c591a960	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 10:29:22.398817+00	2026-03-26 10:29:22.399157+00
8887779a-01ac-4873-8ac1-3c4822e7ea60	4363c0f0-845c-4605-8085-5eb5afef1426	\N	e809a5a917cc9a0f83deaf275fcc60b024920599014965b40cc5a235a16a968e	172.18.0.7	curl/8.17.0	2026-04-26 08:23:58.403377+00	2026-03-27 08:23:58.404247+00
12617720-dda2-48bf-8a54-a8d17eb92157	4363c0f0-845c-4605-8085-5eb5afef1426	\N	a2ea8af21c331059b9f0bd59246d0838223f9dbb2063730e342287abbc1c9b23	172.18.0.7	curl/8.17.0	2026-04-26 08:24:11.161008+00	2026-03-27 08:24:11.161679+00
d0484502-7527-4330-8591-cc16d020033a	4363c0f0-845c-4605-8085-5eb5afef1426	\N	eaf431a08908cd5e6b7196a8c999030e46e7ff4f121d70cb8a1acca1a688df09	172.18.0.7	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-25 10:36:42.993686+00	2026-03-26 10:36:42.994041+00
063a5913-dae8-4a26-9d3c-d134f64bd847	4363c0f0-845c-4605-8085-5eb5afef1426	\N	c8fe3e3f41c1b5409c1f4ad14fb30b1848b007a8074d179d0e9ba20430b06f0b	172.18.0.7	curl/8.17.0	2026-04-25 12:05:06.888153+00	2026-03-26 12:05:06.888741+00
2378524a-4cd1-4238-87a1-94805e46b604	4363c0f0-845c-4605-8085-5eb5afef1426	\N	f0ae11e6f5972f405dd307b2854c191c297da73e271917cd1fea23c2017ece0a	172.18.0.7	curl/8.17.0	2026-04-25 12:05:11.737259+00	2026-03-26 12:05:11.737789+00
c8bbff3b-3234-44a5-96ab-16e5c211e407	4363c0f0-845c-4605-8085-5eb5afef1426	\N	3b14f86abbdb201a182800d33e9bb29aaa363a822060fc97483f3e7aeb4ce1bc	172.18.0.8	curl/8.17.0	2026-04-26 13:18:05.012038+00	2026-03-27 13:18:05.013561+00
faceb74e-615f-4174-ab18-743f7b6ca1e6	4363c0f0-845c-4605-8085-5eb5afef1426	\N	cc39f61153f0fdd8e6f788e2f58e797303f26831f6cb0cf7b1339fe54871c6c3	172.18.0.8	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-26 14:47:18.756357+00	2026-03-27 14:47:18.756618+00
476821f5-2fb4-40f6-b00e-06e52c27de6f	17e778a6-32de-43a9-87bf-dd9e7a5b8c3a	\N	6456981e73d83a76c77205495ec17935a106810004afac9c28799aec64c86628	172.18.0.8	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-26 14:47:48.815797+00	2026-03-27 14:47:48.81611+00
8201a365-efb3-4560-b6b9-94c43bac465e	17e778a6-32de-43a9-87bf-dd9e7a5b8c3a	\N	5d45bb9fc4320c61f00bd5a6bae06805eb6834e8ed33919a6aa60fbd5c369804	172.18.0.8	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-26 15:05:46.346279+00	2026-03-27 15:05:46.346629+00
5ec68267-ac69-4c08-876b-6079d468f278	4363c0f0-845c-4605-8085-5eb5afef1426	\N	8c1f276f4f3a0186ff80e2f59dff619891e273123540eb03e070f066cee2f122	172.18.0.8	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-26 15:05:47.456111+00	2026-03-27 15:05:47.456418+00
ebc25c2f-8cb7-424f-88eb-be12fc839c0b	17e778a6-32de-43a9-87bf-dd9e7a5b8c3a	\N	c1a6e9cd4bd60ba3250e5468b48a19225fdb51b1344e40040d4f2da1f946d25f	172.18.0.8	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-26 14:40:31.228676+00	2026-03-27 14:40:31.229442+00
0c0fc085-97c4-4bfb-97f6-b3d1f0590ab7	91c16381-edb3-46f8-9ca1-7071f49cbc21	\N	6fed77645d7a115833fe7208924a1d556e15d1e737fed74c9d841bd73945499b	172.18.0.8	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-26 15:14:55.019174+00	2026-03-27 15:14:55.019606+00
7afe9a8d-83d7-4e7d-9e5e-28877af8a815	17e778a6-32de-43a9-87bf-dd9e7a5b8c3a	\N	a1a82824549e724bd3a813e48266d5a896aac77c28ff5c89ec12a461b088afa4	172.18.0.8	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-26 15:14:55.088828+00	2026-03-27 15:14:55.089176+00
357fb1ff-fc76-4d28-84da-f95c61cfb1dd	4363c0f0-845c-4605-8085-5eb5afef1426	\N	89fe50a61502fa30dc56f5b7216a1f648649759fcdda21a1727cf4a843f015f9	172.18.0.8	Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36	2026-04-26 15:14:57.001284+00	2026-03-27 15:14:57.001706+00
\.


--
-- Data for Name: users; Type: TABLE DATA; Schema: public; Owner: orbit
--

COPY public.users (id, email, password_hash, phone, display_name, avatar_url, bio, status, custom_status, custom_status_emoji, role, totp_secret, totp_enabled, invited_by, invite_code, last_seen_at, created_at, updated_at) FROM stdin;
4363c0f0-845c-4605-8085-5eb5afef1426	admin@orbit.local	$2b$12$yiwR9eYY8wBO6JA8Pz6JQ.wuvI9ltWu7VTJsiPGqrvbVzcCCgRHuG	\N	Admin	\N	\N	offline	\N	\N	admin	\N	f	\N	\N	\N	2026-03-26 04:01:48.170327+00	2026-03-27 13:17:56.923981+00
7bbb72d1-9635-4f81-8c93-afd4962a9bdb	user1@orbit.local	$2b$12$yiwR9eYY8wBO6JA8Pz6JQ.wuvI9ltWu7VTJsiPGqrvbVzcCCgRHuG	\N	���� ����	\N	\N	offline	\N	\N	member	\N	f	4363c0f0-845c-4605-8085-5eb5afef1426	91249b90	\N	2026-03-26 04:07:27.148092+00	2026-03-27 13:17:56.923981+00
91c16381-edb3-46f8-9ca1-7071f49cbc21	testuser@orbit.local	$2b$12$yiwR9eYY8wBO6JA8Pz6JQ.wuvI9ltWu7VTJsiPGqrvbVzcCCgRHuG	\N	Test User	\N	\N	offline	\N	\N	member	M65MAYRA2K77ZE5DRBRN5CXAMXUO2FTK	f	4363c0f0-845c-4605-8085-5eb5afef1426	d0e0fc66	\N	2026-03-26 04:14:27.386815+00	2026-03-27 13:17:56.923981+00
17e778a6-32de-43a9-87bf-dd9e7a5b8c3a	batalov94@gmail.com	$2a$12$s7nhXlt1cRzB1F5QuRelQOB2z8Z0XsGW9uHbQkwDU3lhH0ZKU4NzO	\N	Batalov	\N	\N	offline	\N	\N	member	\N	f	4363c0f0-845c-4605-8085-5eb5afef1426	2ceaf1eb	\N	2026-03-27 14:23:22.710297+00	2026-03-27 14:23:22.710297+00
\.


--
-- Name: messages_seq; Type: SEQUENCE SET; Schema: public; Owner: orbit
--

SELECT pg_catalog.setval('public.messages_seq', 180, true);


--
-- Name: chat_members chat_members_pkey; Type: CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.chat_members
    ADD CONSTRAINT chat_members_pkey PRIMARY KEY (chat_id, user_id);


--
-- Name: chats chats_pkey; Type: CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.chats
    ADD CONSTRAINT chats_pkey PRIMARY KEY (id);


--
-- Name: devices devices_pkey; Type: CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.devices
    ADD CONSTRAINT devices_pkey PRIMARY KEY (id);


--
-- Name: direct_chat_lookup direct_chat_lookup_pkey; Type: CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.direct_chat_lookup
    ADD CONSTRAINT direct_chat_lookup_pkey PRIMARY KEY (user1_id, user2_id);


--
-- Name: invites invites_code_key; Type: CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.invites
    ADD CONSTRAINT invites_code_key UNIQUE (code);


--
-- Name: invites invites_pkey; Type: CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.invites
    ADD CONSTRAINT invites_pkey PRIMARY KEY (id);


--
-- Name: messages messages_pkey; Type: CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_pkey PRIMARY KEY (id);


--
-- Name: sessions sessions_pkey; Type: CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_pkey PRIMARY KEY (id);


--
-- Name: users users_email_key; Type: CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_email_key UNIQUE (email);


--
-- Name: users users_phone_key; Type: CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_phone_key UNIQUE (phone);


--
-- Name: users users_pkey; Type: CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_pkey PRIMARY KEY (id);


--
-- Name: idx_chat_members_user; Type: INDEX; Schema: public; Owner: orbit
--

CREATE INDEX idx_chat_members_user ON public.chat_members USING btree (user_id);


--
-- Name: idx_devices_user; Type: INDEX; Schema: public; Owner: orbit
--

CREATE INDEX idx_devices_user ON public.devices USING btree (user_id);


--
-- Name: idx_messages_chat_created; Type: INDEX; Schema: public; Owner: orbit
--

CREATE INDEX idx_messages_chat_created ON public.messages USING btree (chat_id, created_at DESC);


--
-- Name: idx_messages_chat_seq; Type: INDEX; Schema: public; Owner: orbit
--

CREATE INDEX idx_messages_chat_seq ON public.messages USING btree (chat_id, sequence_number DESC);


--
-- Name: idx_sessions_token; Type: INDEX; Schema: public; Owner: orbit
--

CREATE INDEX idx_sessions_token ON public.sessions USING btree (token_hash);


--
-- Name: idx_sessions_user; Type: INDEX; Schema: public; Owner: orbit
--

CREATE INDEX idx_sessions_user ON public.sessions USING btree (user_id);


--
-- Name: idx_users_email; Type: INDEX; Schema: public; Owner: orbit
--

CREATE INDEX idx_users_email ON public.users USING btree (email);


--
-- Name: chats trg_chats_updated_at; Type: TRIGGER; Schema: public; Owner: orbit
--

CREATE TRIGGER trg_chats_updated_at BEFORE UPDATE ON public.chats FOR EACH ROW EXECUTE FUNCTION public.update_updated_at();


--
-- Name: users trg_users_updated_at; Type: TRIGGER; Schema: public; Owner: orbit
--

CREATE TRIGGER trg_users_updated_at BEFORE UPDATE ON public.users FOR EACH ROW EXECUTE FUNCTION public.update_updated_at();


--
-- Name: chat_members chat_members_chat_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.chat_members
    ADD CONSTRAINT chat_members_chat_id_fkey FOREIGN KEY (chat_id) REFERENCES public.chats(id) ON DELETE CASCADE;


--
-- Name: chat_members chat_members_last_read_message_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.chat_members
    ADD CONSTRAINT chat_members_last_read_message_id_fkey FOREIGN KEY (last_read_message_id) REFERENCES public.messages(id) ON DELETE SET NULL;


--
-- Name: chat_members chat_members_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.chat_members
    ADD CONSTRAINT chat_members_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: chats chats_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.chats
    ADD CONSTRAINT chats_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id);


--
-- Name: devices devices_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.devices
    ADD CONSTRAINT devices_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: direct_chat_lookup direct_chat_lookup_chat_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.direct_chat_lookup
    ADD CONSTRAINT direct_chat_lookup_chat_id_fkey FOREIGN KEY (chat_id) REFERENCES public.chats(id) ON DELETE CASCADE;


--
-- Name: direct_chat_lookup direct_chat_lookup_user1_fkey; Type: FK CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.direct_chat_lookup
    ADD CONSTRAINT direct_chat_lookup_user1_fkey FOREIGN KEY (user1_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: direct_chat_lookup direct_chat_lookup_user2_fkey; Type: FK CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.direct_chat_lookup
    ADD CONSTRAINT direct_chat_lookup_user2_fkey FOREIGN KEY (user2_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: invites invites_created_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.invites
    ADD CONSTRAINT invites_created_by_fkey FOREIGN KEY (created_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: invites invites_used_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.invites
    ADD CONSTRAINT invites_used_by_fkey FOREIGN KEY (used_by) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: messages messages_chat_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_chat_id_fkey FOREIGN KEY (chat_id) REFERENCES public.chats(id) ON DELETE CASCADE;


--
-- Name: messages messages_forwarded_from_fkey; Type: FK CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_forwarded_from_fkey FOREIGN KEY (forwarded_from) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: messages messages_reply_to_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_reply_to_id_fkey FOREIGN KEY (reply_to_id) REFERENCES public.messages(id);


--
-- Name: messages messages_sender_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.messages
    ADD CONSTRAINT messages_sender_id_fkey FOREIGN KEY (sender_id) REFERENCES public.users(id) ON DELETE SET NULL;


--
-- Name: sessions sessions_device_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_device_id_fkey FOREIGN KEY (device_id) REFERENCES public.devices(id) ON DELETE SET NULL;


--
-- Name: sessions sessions_user_id_fkey; Type: FK CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.sessions
    ADD CONSTRAINT sessions_user_id_fkey FOREIGN KEY (user_id) REFERENCES public.users(id) ON DELETE CASCADE;


--
-- Name: users users_invited_by_fkey; Type: FK CONSTRAINT; Schema: public; Owner: orbit
--

ALTER TABLE ONLY public.users
    ADD CONSTRAINT users_invited_by_fkey FOREIGN KEY (invited_by) REFERENCES public.users(id);


--
-- PostgreSQL database dump complete
--

\unrestrict 18gVG98M1tcavCFdT5IKvnEsjzdeav52Y77MrIrTez6Obkl1e7edluEVj80M1OA

