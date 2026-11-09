(ns bifrost.store
  (:refer-clojure :exclude [get next map])
  (:require ["node:sqlite" :as sqlite]
            ["node:fs" :as fs]
            ["node:path" :as path]
            ["node:crypto" :as crypto]
            [bifrost.runtime :as runtime]))

(def schema
  "PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;
   CREATE TABLE IF NOT EXISTS kv (kind TEXT, id TEXT, value TEXT NOT NULL, PRIMARY KEY(kind,id));
   CREATE TABLE IF NOT EXISTS jobs (id INTEGER PRIMARY KEY, dedup TEXT UNIQUE NOT NULL,
     stream TEXT NOT NULL, type TEXT NOT NULL, payload TEXT NOT NULL, state TEXT NOT NULL DEFAULT 'pending',
     attempts INTEGER NOT NULL DEFAULT 0, due INTEGER NOT NULL DEFAULT 0, error TEXT, created INTEGER NOT NULL);
   CREATE INDEX IF NOT EXISTS jobs_ready ON jobs(state,due,id);
   CREATE TABLE IF NOT EXISTS effects (id TEXT PRIMARY KEY, state TEXT NOT NULL, result TEXT);
   CREATE TABLE IF NOT EXISTS messages (wa_chat TEXT NOT NULL, wa_id TEXT NOT NULL, tg_chat TEXT NOT NULL,
     tg_id INTEGER NOT NULL, bot TEXT NOT NULL, direction TEXT NOT NULL, media INTEGER NOT NULL DEFAULT 0,
     PRIMARY KEY(bot,tg_chat,tg_id));
   CREATE INDEX IF NOT EXISTS messages_wa ON messages(wa_chat,wa_id);
   CREATE TABLE IF NOT EXISTS aliases (alias TEXT PRIMARY KEY, canonical TEXT NOT NULL);
   CREATE TABLE IF NOT EXISTS telegram_messages (
     seq INTEGER PRIMARY KEY, scope TEXT NOT NULL, chat TEXT NOT NULL, message INTEGER NOT NULL,
     bot TEXT NOT NULL, thread INTEGER NOT NULL DEFAULT 0, kind TEXT NOT NULL,
     date INTEGER NOT NULL, expires INTEGER NOT NULL DEFAULT 0, deleted INTEGER NOT NULL DEFAULT 0, error TEXT,
     UNIQUE(scope,chat,message));
   CREATE INDEX IF NOT EXISTS telegram_messages_chat ON telegram_messages(scope,chat,thread,seq);")

(defn open!
  ([filename key] (open! filename key {}))
  ([filename key {:keys [recover now] :or {recover false now js/Date.now}}]
   (runtime/assert-bun!)
   (when (not= filename ":memory:")
     (fs/mkdirSync (path/dirname filename) #js {:recursive true :mode 448}))
   (let [db (sqlite/DatabaseSync. filename)]
     (try
       (.exec db schema)
       (when recover
         (.exec db "UPDATE effects SET state='uncertain' WHERE state='sending'; UPDATE jobs SET state='pending' WHERE state='running'"))
       {:db db :key (js/Buffer.from key "hex") :now now}
       (catch :default error (.close db) (throw error))))))

(defn close! [{:keys [db]}] (.close db))

;; Values stay as JS objects at the storage boundary. This retains property
;; order and absent/null distinctions for existing JSON/hash input bytes.
(defn get [{:keys [db]} kind id]
  (let [row (.get (.prepare db "SELECT value FROM kv WHERE kind=? AND id=?") kind id)]
    (if row (js/JSON.parse (.-value row)) js/undefined)))

(defn put! [{:keys [db]} kind id value]
  (.run (.prepare db "INSERT INTO kv VALUES(?,?,?) ON CONFLICT(kind,id) DO UPDATE SET value=excluded.value")
        kind id (js/JSON.stringify value)))

(defn all [{:keys [db]} kind]
  (.map (.all (.prepare db "SELECT id,value FROM kv WHERE kind=?") kind)
        (fn [row] (js/Object.assign #js {} (js/JSON.parse (.-value row)) #js {:id (.-id row)}))))

(defn canonical [{:keys [db]} id]
  (let [row (.get (.prepare db "SELECT canonical FROM aliases WHERE alias=?") id)]
    (if (and row (js/Boolean (.-canonical row))) (.-canonical row) (.replace id "@s.whatsapp.net" "@c.us"))))

(defn alias! [{:keys [db]} alias canonical]
  (let [existing (.get (.prepare db "SELECT canonical FROM aliases WHERE alias=?") alias)]
    (when (and existing (not= (.-canonical existing) canonical))
      (throw (js/Error. "Conflicting contact identity; manual review required")))
    (.run (.prepare db "INSERT OR IGNORE INTO aliases VALUES(?,?)") alias canonical)))

(defn enqueue!
  ([store dedup stream type payload] (enqueue! store dedup stream type payload 0))
  ([{:keys [db now]} dedup stream type payload due]
   (js/Boolean (.-changes (.run (.prepare db "INSERT OR IGNORE INTO jobs(dedup,stream,type,payload,created,due) VALUES(?,?,?,?,?,?)")
                               dedup stream type (js/JSON.stringify payload) (now) due)))))

(def ready-query
  "SELECT * FROM jobs j WHERE state='pending' AND due<=?
   AND NOT EXISTS (SELECT 1 FROM jobs p WHERE p.stream=j.stream AND p.id<j.id AND p.state IN ('pending','running','uncertain','failed'))
   AND (j.type!='sync-finish' OR NOT EXISTS (SELECT 1 FROM jobs t WHERE t.type IN ('sync-start','sync-target')
     AND json_extract(t.payload,'$.run')=json_extract(j.payload,'$.run') AND t.state IN ('pending','running')))
   AND (j.type!='wa' OR NOT EXISTS (SELECT 1 FROM kv h WHERE h.kind='history-run'
     AND json_extract(h.value,'$.status') IN ('queued','fetching','delivering')
     AND json_extract(h.value,'$.session')=coalesce(json_extract(j.payload,'$.accountId'),
       (SELECT json_extract(value,'$.id') FROM kv WHERE kind='identity' AND id='account'),json_extract(j.payload,'$.sessionId'))
     AND json_extract(h.value,'$.chatId')=coalesce((SELECT canonical FROM aliases WHERE alias=json_extract(j.payload,'$.data.chatId')),
       replace(json_extract(j.payload,'$.data.chatId'),'@s.whatsapp.net','@c.us'))))
   ORDER BY CASE WHEN j.type='history' OR j.type LIKE 'history-%' THEN 3 WHEN j.type LIKE 'control-%' THEN 2 WHEN j.type LIKE 'sync-%' OR j.type LIKE 'status-%' THEN 1 ELSE 0 END,id LIMIT 1")

(defn next! [{:keys [db now]}]
  (let [row (.get (.prepare db ready-query) (now))]
    (if row
      (do (.run (.prepare db "UPDATE jobs SET state='running',attempts=attempts+1 WHERE id=?") (.-id row))
          (js/Object.assign #js {} row #js {:attempts (inc (js/Number (.-attempts row)))
                                           :payload (js/JSON.parse (.-payload row))}))
      js/undefined)))

(defn finish! [{:keys [db]} id]
  (.run (.prepare db "UPDATE jobs SET state='done',error=NULL WHERE id=?") id))

(defn fail!
  ([store id state error] (fail! store id state error 0))
  ([{:keys [db now]} id state error delay]
   (.run (.prepare db "UPDATE jobs SET state=?,error=?,due=? WHERE id=?") state (.slice error 0 500) (+ (now) delay) id)))

(defn map!
  ([store wa-chat wa-id tg-chat tg-id bot direction] (map! store wa-chat wa-id tg-chat tg-id bot direction false))
  ([{:keys [db]} wa-chat wa-id tg-chat tg-id bot direction media]
   (.run (.prepare db "INSERT OR IGNORE INTO messages VALUES(?,?,?,?,?,?,?)")
         wa-chat wa-id (js/String tg-chat) tg-id bot direction (js/Number media))))

(defn by-telegram
  ([store chat message] (by-telegram store chat message js/undefined))
  ([{:keys [db]} chat message bot]
   (if (and (js/Boolean bot) (not (.startsWith (js/String chat) "-")))
     (.get (.prepare db "SELECT * FROM messages WHERE tg_chat=? AND tg_id=? AND bot=?") (js/String chat) message bot)
     (.get (.prepare db "SELECT * FROM messages WHERE tg_chat=? AND tg_id=?") (js/String chat) message))))

(defn by-whatsapp [{:keys [db]} chat message]
  (.all (.prepare db "SELECT * FROM messages WHERE wa_chat=? AND wa_id=? ORDER BY rowid") chat message))

(defn seal [{:keys [key]} text]
  (let [iv (crypto/randomBytes 12)
        cipher (crypto/createCipheriv "aes-256-gcm" key iv)]
    (.toString (js/Buffer.concat #js [iv (.update cipher text) (.final cipher) (.getAuthTag cipher)]) "base64")))

(defn unseal [{:keys [key]} text]
  (let [bytes (js/Buffer.from text "base64")
        decipher (crypto/createDecipheriv "aes-256-gcm" key (.subarray bytes 0 12))]
    (.setAuthTag decipher (.subarray bytes -16))
    (.toString (js/Buffer.concat #js [(.update decipher (.subarray bytes 12 -16)) (.final decipher)]))))
