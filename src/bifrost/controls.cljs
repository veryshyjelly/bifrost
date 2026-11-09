(ns bifrost.controls
  (:require [bifrost.store :as store]
            [bifrost.routing :as routing]
            [bifrost.wire :as w]))

(defn command-of [message]
  (let [text (.trim (w/first-truthy (.-text message) (.-caption message) ""))
        match (.exec #"(?i)^/([a-z0-9_]+)(?:@([a-z0-9_]+))?(?:\s+([\s\S]*))?$" text)]
    (if match
      #js {:name (.toLowerCase (aget match 1))
           :recipient (if (w/truthy? (aget match 2)) (.toLowerCase (aget match 2)) js/undefined)
           :args (into-array (remove empty? (.split (.trim (w/first-truthy (aget match 3) "")) #"\s+")))}
      js/undefined)))

(def service-pattern
  #"^(new_chat_participant|new_chat_member|new_chat_members|left_chat_participant|left_chat_member|new_chat_title|new_chat_photo|delete_chat_photo|group_chat_created|supergroup_chat_created|channel_chat_created|pinned_message|migrate_to_chat_id|migrate_from_chat_id|message_auto_delete_timer_changed|forum_topic_|general_forum_topic_|video_chat_|write_access_allowed|users_shared|chat_shared|boost_added|chat_background_set|community_chat_joined)")

(defn service-message? [message] (boolean (some #(re-find service-pattern %) (js/Object.keys message))))

(defn track!
  ([dependencies bot message kind] (track! dependencies bot message kind 0))
  ([{:keys [store] :as dependencies} bot message kind expires]
   (when (and (js/Number.isSafeInteger (.-message_id message)) (w/truthy? (w/at message "chat" "id")))
     (let [chat (js/String (w/at message "chat" "id")) scope (routing/scope bot chat)
           {:keys [thread]} (routing/telegram-context dependencies bot message)
           db (:db store)]
       (.run (.prepare db "INSERT INTO telegram_messages(scope,chat,message,bot,thread,kind,date,expires) VALUES(?,?,?,?,?,?,?,?)
         ON CONFLICT(scope,chat,message) DO UPDATE SET kind=CASE WHEN telegram_messages.kind IN ('chat','command') THEN telegram_messages.kind ELSE excluded.kind END,
         expires=CASE WHEN telegram_messages.kind='chat' THEN 0 ELSE max(telegram_messages.expires,excluded.expires) END")
             scope chat (.-message_id message) bot (w/number-or-zero thread) kind
             (if (w/truthy? (.-date message)) (* 1000 (.-date message)) ((:now store))) expires)
       (when (w/truthy? expires)
         (let [row (.get (.prepare db "SELECT * FROM telegram_messages WHERE scope=? AND chat=? AND message=?") scope chat (.-message_id message))]
           (when (not= "chat" (w/get row "kind"))
             (let [key (str "control-expire:" scope ":" chat ":" (.-message_id message))]
               (store/enqueue! store key key "control-expire"
                               #js {:scope scope :chat chat :message (.-message_id message) :bot (if (.startsWith chat "-") "master" bot)} expires)))))))))

(defn observe! [{:keys [store config] :as dependencies} bot message]
  (let [protected (w/first-truthy (.-supergroup_chat_created message) (.-channel_chat_created message) (.-forum_topic_created message))
        sender (js/String (w/at message "from" "id"))
        master (js/String (w/first-truthy (w/get (store/get store "telegram-identity" "master") "id")
                                         (aget (.split (:masterToken config) ":") 0)))
        bridge-bot (and (w/truthy? (w/at message "from" "is_bot"))
                        (or (= sender master) (store/get store "bot" sender)))
        kind (cond (w/truthy? protected) "protected" (service-message? message) "service"
                   bridge-bot "log" (command-of message) "command" :else "chat")]
    (track! dependencies bot message kind)))

(defn reply! [{:keys [store config] :as dependencies} bot receipt extra]
  (let [message (if (w/truthy? (.-message_thread_id extra))
                  (w/object receipt #js {:message_thread_id (.-message_thread_id extra) :is_topic_message true}) receipt)
        ttl (if (w/truthy? (.-reply_markup extra)) (:controlTTL config) (:commandTTL config))]
    (track! dependencies bot message "log" (+ ((:now store)) (* 1000 ttl)))))
