(ns bifrost.config
  (:require ["node:crypto" :as crypto]
            [bifrost.cron :as cron]))

(defn equal? [a b]
  (let [x (js/Buffer.from a) y (js/Buffer.from b)]
    (and (= (.-length x) (.-length y)) (crypto/timingSafeEqual x y))))

(defn digest [value] (.digest (.update (crypto/createHash "sha256") value) "hex"))
(defn sign [value key] (.digest (.update (crypto/createHmac "sha256" key) value) "hex"))

(defn configuration
  ([] (configuration js/process.env))
  ([env]
   (let [value (fn [key fallback] (let [v (aget env key)] (if (js/Boolean v) v fallback)))
         number (fn [key fallback] (js/Number (value key fallback)))]
     {:port (number "BRIDGE_PORT" 3210)
      :db (value "BRIDGE_DB" "./data/bridge.db") :media (value "BRIDGE_MEDIA" "./media")
      :key (value "BRIDGE_ENCRYPTION_KEY" "") :admin (value "BRIDGE_ADMIN_TOKEN" "")
      :webhookSecret (value "OPENWA_WEBHOOK_SECRET" "")
      :owner (value "TELEGRAM_OWNER_ID" "") :management (value "TELEGRAM_MANAGEMENT_CHAT_ID" "")
      :masterToken (value "TELEGRAM_MASTER_TOKEN" "") :telegram (value "TELEGRAM_API_URL" "http://telegram-api:8081")
      :callback (value "BRIDGE_CALLBACK_URL" "http://bifrost:3210") :openwa (value "OPENWA_URL" "http://openwa-api:2785")
      :openwaKey (value "OPENWA_API_KEY" "") :session (value "OPENWA_SESSION_ID" "") :account (value "BIFROST_ACCOUNT_ID" "")
      :history (js/Math.min 2000 (js/Math.max 0 (number "HISTORY_LIMIT" 20)))
      :mediaLimit (number "MAX_MEDIA_BYTES" (* 100 1024 1024)) :pace (number "DELIVERY_INTERVAL_MS" 1100)
      :syncCron (value "SYNC_CRON" "0 * * * *") :syncTimezone (value "SYNC_TIMEZONE" "Asia/Kolkata")
      :commandTTL (number "COMMAND_TTL_SECONDS" 60) :controlTTL (number "CONTROL_TTL_SECONDS" 600)})))

(defn- require! [valid message] (when-not valid (throw (js/Error. message))))

(defn validate! [c]
  (doseq [key [:key :admin :webhookSecret]]
    (require! (>= (.-length (key c)) 32) (str (name key) " must contain at least 32 characters")))
  (require! (re-matches #"(?i)[a-f0-9]{64}" (:key c)) "BRIDGE_ENCRYPTION_KEY must be 32 bytes in hex")
  (require! (and (js/Number.isSafeInteger (:port c)) (<= 1 (:port c) 65535)) "Invalid port")
  (require! (and (js/Number.isFinite (:mediaLimit c)) (>= (:mediaLimit c) 1024)) "Invalid media limit")
  (require! (and (js/Number.isSafeInteger (:history c)) (>= (:history c) 0)) "Invalid history limit")
  (require! (or (not (js/Boolean (:owner c))) (re-matches #"\d+" (:owner c))) "TELEGRAM_OWNER_ID must be your numeric user ID")
  (require! (or (not (js/Boolean (:management c))) (re-matches #"-\d+" (:management c))) "TELEGRAM_MANAGEMENT_CHAT_ID must be a group ID")
  (require! (and (js/Number.isFinite (:pace c)) (>= (:pace c) 100)) "DELIVERY_INTERVAL_MS must be at least 100")
  (cron/schedule (:syncCron c) (:syncTimezone c))
  (doseq [ttl [(:commandTTL c) (:controlTTL c)]]
    (require! (and (js/Number.isSafeInteger ttl) (<= 5 ttl 86400)) "Command lifetimes must be 5–86400 seconds")))
