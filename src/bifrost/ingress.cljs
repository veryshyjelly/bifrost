(ns bifrost.ingress
  (:require [bifrost.store :as store]
            [bifrost.config :as config]
            [bifrost.api :as api]
            [bifrost.routing :as routing]
            [bifrost.controls :as controls]
            [bifrost.wire :as w]))

(def wa-events
  #{"message.received" "message.sent" "message.edited" "message.revoked" "message.reaction"
    "poll.vote" "status.received" "session.status" "session.disconnected" "message.failed"})

(defn accept-whatsapp! [{:keys [store config]} payload]
  (when (or (not= (:session config) (.-sessionId payload))
            (not (contains? wa-events (.-event payload)))
            (not (w/truthy? (.-idempotencyKey payload))) (not (w/truthy? (.-data payload))))
    (throw (api/api-error "Unexpected OpenWA event or session" 400 false false 5000)))
  (let [stream (if (= "status.received" (.-event payload)) "status@broadcast" (w/first-truthy (w/at payload "data" "chatId") "session"))]
    (store/enqueue! store (str "ow:" (js/String (.-idempotencyKey payload))) (str "wa:" stream) "wa"
                    (w/object payload #js {:accountId (:account config)}))))

(defn accept-telegram! [{:keys [store config] :as dependencies} bot update]
  (when (and (not= bot "master") (not (store/get store "bot" bot)))
    (throw (api/api-error "Unknown bot" 404 false false 5000)))
  (let [observed (w/first-truthy (.-message update) (.-edited_message update))]
    (when (and (w/truthy? (w/get observed "chat"))
               (or (= (js/String (w/at observed "chat" "id")) (:owner config))
                   (and (not= "private" (w/at observed "chat" "type"))
                        (or (= (js/String (w/at observed "chat" "id")) (:management config))
                            (:route (routing/telegram-context dependencies bot observed))))))
      (controls/observe! dependencies bot observed)))
  (let [actor (w/first-truthy (w/at update "callback_query" "from") (w/at update "message" "from")
                              (w/at update "edited_message" "from") (w/at update "message_reaction" "user")
                              (w/at update "my_chat_member" "from") (w/at update "managed_bot" "user")
                              (w/at update "poll_answer" "user"))]
    (if (or (not (w/truthy? actor)) (w/truthy? (.-is_bot actor)) (not= (js/String (.-id actor)) (:owner config)))
      false
      (do
        (when-not (js/Number.isSafeInteger (.-update_id update))
          (throw (api/api-error "Invalid update" 400 false false 5000)))
        (let [message (w/first-truthy (.-message update) (.-edited_message update) (w/at update "callback_query" "message"))
              reaction (.-message_reaction update)
              recipient (when (w/truthy? message) (w/get (controls/command-of message) "recipient"))
              username (w/get (store/get store "bot" bot) "username")
              directed (and (w/truthy? recipient) (= recipient (when (w/truthy? username) (.toLowerCase username))))
              chat (w/first-truthy (w/get message "chat") (w/get reaction "chat"))]
          (if (and (not= "private" (w/get chat "type")) (not= bot "master")
                   (not (w/truthy? (.-my_chat_member update))) (not (w/truthy? (.-callback_query update))) (not directed))
            false
            (let [stream (cond
                           (w/truthy? (w/get message "chat"))
                           (str "tg:" (when (= "private" (w/at message "chat" "type")) (str bot ":"))
                                (js/String (w/at message "chat" "id")) ":" (w/first-truthy (:thread (routing/telegram-context dependencies bot message)) 0))
                           (w/truthy? (w/get reaction "chat")) (str "tg-reaction:" bot ":" (js/String (w/at reaction "chat" "id")))
                           :else (str "tg:" bot))]
              (store/enqueue! store (str "tg:" bot ":" (.-update_id update)) stream "tg" #js {:bot bot :update update}))))))))

(defn management! [{:keys [store]} type data dedup]
  (store/enqueue! store (str "management:" (js/String dedup)) (str "management:" (js/String dedup)) "management"
                  #js {:id dedup :type type :data data :at ((:now store))}))

(defn accept-management! [{:keys [store config]} event]
  (let [signature (.-signature event)
        body (w/object event)]
    (js/Reflect.deleteProperty body "signature")
    (when (or (not (string? signature))
              (not (config/equal? signature (config/sign (js/JSON.stringify body) (:admin config))))
              (> (js/Math.abs (- ((:now store)) (.-at body))) (* 7 86400000)))
      (throw (api/api-error "Invalid management signature" 401 false false 5000)))
    (store/enqueue! store (str "management:" (js/String (.-id body))) (str "management:" (js/String (.-id body))) "management" body)))

(defn status [{:keys [store config]}]
  (let [db (:db store)]
    #js {:configured (boolean (every? w/truthy? (map config [:masterToken :owner :management :openwaKey :session])))
         :bots (.-length (store/all store "bot")) :conversations (.-length (store/all store "route"))
         :metadataSync #js {:cron (:syncCron config) :timezone (:syncTimezone config) :last (w/first-truthy (store/get store "sync-status" "last") nil)}
         :telegramControls #js {:commandTTLSeconds (:commandTTL config) :buttonTTLSeconds (:controlTTL config)
                               :cleanupProblems (.-n (.get (.prepare db "SELECT count(*) AS n FROM telegram_messages WHERE deleted=0 AND error IS NOT NULL")))}
         :jobs (.all (.prepare db "SELECT state,count(*) AS count FROM jobs GROUP BY state"))}))
