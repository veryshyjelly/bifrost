(ns bifrost.relay
  (:require [bifrost.store :as store]
            [bifrost.config :as config]
            [bifrost.api :as api]
            [bifrost.effects :as effects]
            [bifrost.routing :as routing]
            [bifrost.controls :as controls]
            [bifrost.ingress :as ingress]
            [bifrost.conversations :as conversations]
            [bifrost.copies :as copies]
            [bifrost.mentions :as mentions]
            [bifrost.format :as format]
            [bifrost.text :as text]
            [bifrost.wire :as w]
            [promesa.core :as p]))

(defn workflow! [dependencies workflow & args]
  ;; Unfinished workflows must never be acknowledged as successful work. The
  ;; production entry point stays on the original until the inventory is done.
  (if-let [f (get-in dependencies [:workflows workflow])]
    (apply f args)
    (p/rejected (ex-info (str "Candidate workflow is not implemented: " (name workflow)) {:workflow workflow}))))

(defn- source! [{:keys [store config]} event]
  (let [original (.-data event)]
    (when (and (w/truthy? (.-id original)) (w/truthy? (.-chatId original))
               (not (w/truthy? (.-isStatusBroadcast original))) (not (#{"channel" "status" "broadcast"} (.-kind original))))
      (let [raw-sender (if (w/truthy? (.-isGroup original)) (w/first-truthy (.-author original) (.-from original)) (.-chatId original))
            sender (if (and (w/truthy? (.-senderPhone original)) (not (w/truthy? (.-fromMe original))))
                     (str (.-senderPhone original) "@c.us") (store/canonical store raw-sender))
            _ (when (and (w/truthy? (.-senderPhone original)) (string? raw-sender) (.endsWith raw-sender "@lid"))
                (store/alias! store raw-sender sender))
            chat (store/canonical store (.-chatId original)) id (str chat ":" (.-id original))]
        (when-not (store/get store "revoked" id)
          (let [edit (store/get store "latest-edit" id)
                message (if edit
                          (w/object original #js {:body (.-body edit)} (when (w/truthy? (.-mentionedIds edit)) #js {:mentionedIds (.-mentionedIds edit)})) original)]
            (when-not (or (store/get store "wa-complete" id) (some #(= "out" (.-direction %)) (store/by-whatsapp store chat (.-id message))))
              (let [contact (w/first-truthy (store/get store "contact" sender) #js {})
                    name (if (w/truthy? (.-metadataSyncedAt contact)) (.-name contact)
                             (w/first-truthy (w/at message "contact" "name") (w/at message "contact" "pushName")
                                             (w/at message "contact" "shortName") (.-name contact) sender))
                    link-sender (if (w/truthy? (.-fromMe message))
                                  (w/first-truthy (w/get (store/get store "reaction-self" (config/digest (:account config))) "sender") "")
                                  (if (w/truthy? (.-senderPhone message)) (str (.-senderPhone message) "@c.us") sender))]
                (store/put! store "contact" sender (w/object contact #js {:name name}))
                (store/put! store "wamessage" id #js {:sender sender :name name :fromMe (w/truthy? (.-fromMe message))
                                                     :historical (w/truthy? (.-historical event)) :timestamp (.-timestamp message) :status false
                                                     :linkSender (store/canonical store link-sender) :mentionedIds (w/first-truthy (.-mentionedIds message) #js [])})
                {:message message :chat chat :sender sender}))))))))

(defn- select-target! [{:keys [store config api] :as dependencies} event key {:keys [message chat sender]}]
  (p/let [initial-route (conversations/topic! dependencies chat
                                             (if (w/truthy? (.-isGroup message)) (w/first-truthy (.-chatName message) chat)
                                                 (.-name (store/get store "contact" sender))) key)]
    (when-not (#{"muted" "ignored"} (.-mode initial-route))
      (let [private? (= "dedicated" (.-mode initial-route))
            snapshot-key (str key (when (w/truthy? (.-forceMaster event)) ":fallback"))
            previous (store/get store "delivery-target" snapshot-key)
            previous (when-not (and (w/truthy? (w/get previous "thread"))
                                    (store/get store "retired-topic" (config/digest (js/JSON.stringify #js [(js/String (.-destination previous)) (.-thread previous)])))) previous)
            legacy? (and previous (not= 2 (.-formatVersion previous)))]
        (p/let [bot (if (w/truthy? (w/get previous "bot")) (.-bot previous)
                       (if (or (and (w/truthy? (.-fromMe message)) (not private?)) (w/truthy? (.-forceMaster event)))
                         "master" (routing/preferred! dependencies sender (.-tgChat initial-route) private?)))
                route (if (and (= "master" bot) (w/truthy? (.-inboxRetired initial-route)))
                        (p/let [topic (effects/run! store (str key ":restore-inbox")
                                                    #((:tg api) (routing/token dependencies "master") "createForumTopic"
                                                      #js {:chat_id (:management config) :name (.slice (js/String (w/first-truthy (.-name initial-route) "WhatsApp conversation")) 0 128)}))]
                          (let [route (w/object initial-route #js {:tgChat (:management config) :thread (.-message_thread_id topic) :inboxRetired false})]
                            (store/put! store "route" chat route) route)) initial-route)]
          (let [destination (w/first-truthy (w/get previous "destination") (if (and private? (not= "master" bot)) (:owner config) (.-tgChat route)))
                thread (if previous (.-thread previous) (if (and private? (not= "master" bot)) js/undefined (.-thread route)))
                delivered-target (w/object #js {:chat (js/String destination) :thread thread}
                                            (when-not (.startsWith (js/String destination) "-") #js {:bot bot}))
                presentation (w/first-truthy (w/get previous "presentation")
                                            (w/object #js {:bot bot :sender (.-linkSender (store/get store "wamessage" (str chat ":" (.-id message))))
                                                            :name (w/first-truthy (w/get (store/get store "contact" sender) "name") sender)
                                                            :own (w/truthy? (.-fromMe message)) :historical (w/truthy? (.-historical event))
                                                            :status false :timestamp (.-timestamp message)}
                                                      (when (w/truthy? (.-historical event))
                                                        #js {:historyStyle (w/first-truthy (.-historyStyle event) "compact")
                                                             :timeZone (w/first-truthy (.-historyTimeZone event) (:syncTimezone config))})))]
            (copies/begin! dependencies chat (.-id message) delivered-target key)
            (store/put! store "delivery-target" snapshot-key #js {:bot bot :destination destination :thread thread :formatVersion (if legacy? 1 2)})
            (when-not legacy?
              (store/put! store "delivery-target" snapshot-key #js {:bot bot :destination destination :thread thread :formatVersion 2 :presentation presentation}))
            {:bot bot :destination destination :thread thread :private? private? :legacy? legacy?
             :delivered-target delivered-target :presentation presentation :key (str key ":to:" bot ":" destination)}))))))

(defn- quote-extra [{:keys [store]} message chat {:keys [bot destination thread private?]}]
  (let [quoted-id (w/at message "quotedMessage" "id")
        quoted (when (w/truthy? quoted-id)
                 (some (fn [row]
                         (when (and (= (.-tg_chat row) (js/String destination))
                                    (or (not (and private? (not= bot "master"))) (= (.-bot row) bot)))
                           (let [record (.get (.prepare (:db store) "SELECT deleted,thread FROM telegram_messages WHERE scope=? AND chat=? AND message=?")
                                              (routing/scope (.-bot row) (.-tg_chat row)) (.-tg_chat row) (.-tg_id row))]
                             (when (or (not record) (and (not (w/truthy? (.-deleted record)))
                                                        (= (w/number-or-zero (.-thread record)) (w/number-or-zero thread)))) row))))
                       (.reverse (store/by-whatsapp store chat quoted-id))))
        extra (w/object (when (w/truthy? thread) #js {:message_thread_id thread})
                        (when quoted #js {:reply_parameters #js {:message_id (.-tg_id quoted) :allow_sending_without_reply true}}))]
    {:extra extra :quoted quoted}))

(defn- send-incoming-text! [{:keys [store] :as dependencies} event {:keys [message chat sender]} selected]
  (let [{:keys [bot destination thread legacy? presentation delivered-target key]} selected
        {:keys [extra quoted]} (quote-extra dependencies message chat selected)
        label (cond (w/truthy? (.-fromMe message)) "You · sent from WhatsApp"
                    (= bot "master") (w/first-truthy (w/get (store/get store "contact" sender) "name") sender) :else "")
        prefix (str (when (and legacy? (w/truthy? (.-historical event)))
                      (str "[History · " (.toISOString (js/Date. (* 1000 (.-timestamp message)))) "]\n"))
                    (when (seq label) (str label "\n")))
        preview (when (and (w/truthy? (.-historical event)) (not quoted) (w/truthy? (w/at message "quotedMessage" "body")))
                  (format/styled (str "↪ " (.join (.slice (js/Array.from (js/String (w/at message "quotedMessage" "body"))) 0 300) "")) "italic"))]
    (p/let [body (if legacy? (format/plain (w/first-truthy (.-body message) ""))
                    (mentions/incoming! dependencies (w/first-truthy (.-body message) "") (.-mentionedIds message) chat (js/String destination) (str key ":body")))
            render (fn [body] (if legacy? (str prefix (:text body))
                                 (format/relay-text (js->clj presentation :keywordize-keys true)
                                                    (if preview (format/concat preview "\n" body) body) ((:now store)))))
            fallback (if (#{"text" "contact"} (.-type message)) ""
                         (str (w/first-truthy (.-type message) "Unsupported message") (when (w/truthy? (w/at message "call" "missed")) " · missed")))
            content (if (empty? fallback) (render body)
                        (if legacy? (str prefix (:text body) "\n[" fallback "]")
                            (render (format/concat body (if (w/truthy? (.-body message)) "\n" "") (format/styled (str "[" fallback "]") "italic")))))
            receipts (text/send! dependencies bot destination content key extra "chat")]
      (doseq [receipt receipts]
        (store/map! store chat (.-id message) destination (.-message_id receipt) bot "in")
        (controls/track! dependencies bot (if (w/truthy? thread) (w/object receipt #js {:message_thread_id thread :is_topic_message true}) receipt) "chat")
        (let [id (config/digest (js/JSON.stringify #js [bot (js/String destination) (.-message_id receipt)]))]
          (store/put! store "telegram-receipt" id receipt)
          (store/put! store "message-presentation" id presentation)))
      (copies/complete! dependencies chat (.-id message) delivered-target)
      (store/put! store "wa-complete" (str chat ":" (.-id message)) true))))

(defn whatsapp! [dependencies event key]
  (let [event-type (.-event event) message (.-data event)]
    (cond
      (w/truthy? (.-replay event)) (workflow! dependencies :history-relay event key)
      (= event-type "status.received") (workflow! dependencies :status-relay event key)
      (or (.startsWith event-type "session.") (= event-type "message.failed"))
      (p/resolved (do (ingress/management! dependencies "session_status" #js {:event event-type :status (w/first-truthy (.-status message) "changed")} key) js/undefined))
      (#{"message.edited" "message.revoked" "message.reaction" "poll.vote"} event-type) (workflow! dependencies :whatsapp-change event key)
      :else
      (if-let [source (source! dependencies event)]
        (p/let [selected (select-target! dependencies event key source)]
          (when selected
            (if (or (#{"image" "video" "audio" "voice" "sticker" "document"} (.-type message))
                    (and (= "location" (.-type message)) (w/truthy? (.-location message)))
                    (and (= "contact" (.-type message)) (re-find #"(?i)BEGIN:VCARD" (w/first-truthy (.-body message) "")))
                    (and (= "poll" (.-type message)) (w/truthy? (.-poll message))))
              (workflow! dependencies :whatsapp-content event key source selected)
              (send-incoming-text! dependencies event source selected))))
        (p/resolved js/undefined)))))

(defn repair-topic! [{:keys [store config api] :as dependencies} event key error]
  (let [data (api/error-data error)]
    (if (or (w/truthy? (.-replay event)) (not= "telegram" (:service data)) (not= 400 (:status data)) (not= "thread_missing" (:reason data)))
      (p/resolved false)
      (let [chat (store/canonical store (w/first-truthy (w/at event "data" "chatId") ""))
            snapshot-key (str key (when (w/truthy? (.-forceMaster event)) ":fallback"))
            target (store/get store "delivery-target" snapshot-key) route (store/get store "route" chat)
            prefix (str key ":to:" (w/get target "bot") ":" (w/get target "destination") ":")]
        (if (or (not (w/truthy? (w/get target "thread"))) (not= "master" (w/get target "bot"))
                (not= (js/String (w/get target "destination")) (:management config)) (not= (js/String (w/get route "tgChat")) (:management config))
                (pos? (.-length (store/by-whatsapp store chat (w/at event "data" "id"))))
                (.get (.prepare (:db store) "SELECT 1 FROM effects WHERE state IN ('done','sending','uncertain') AND substr(id,1,length(?))=? LIMIT 1") prefix prefix))
          (p/resolved false)
          (p/let [bot (when (and (= "dedicated" (.-mode route)) (not (w/truthy? (.-forceMaster event))))
                        (routing/preferred! dependencies chat (.-tgChat route) true))]
            (if (and bot (not= "master" bot))
              (do (store/put! store "delivery-target" snapshot-key
                              (w/object target #js {:bot bot :destination (:owner config) :thread js/undefined}
                                        (when (w/truthy? (.-presentation target)) #js {:presentation (w/object (.-presentation target) #js {:bot bot})}))) true)
              (p/let [thread (if (or (not (w/truthy? (.-thread route))) (= (js/Number (.-thread route)) (js/Number (.-thread target))))
                              (p/let [repaired (effects/run! store (str "topic-repair:" (config/digest (js/JSON.stringify #js [(:management config) chat (.-thread route)])))
                                                             #((:tg api) (routing/token dependencies "master") "createForumTopic"
                                                               #js {:chat_id (:management config) :name (.slice (js/String (w/first-truthy (.-name route) (w/get (store/get store "contact" chat) "name") "WhatsApp conversation")) 0 128)}))]
                                (store/put! store "route" chat (w/object route #js {:thread (.-message_thread_id repaired) :inboxRetired false}))
                                (.-message_thread_id repaired)) (.-thread route))]
                (if (w/truthy? thread) (do (store/put! store "delivery-target" snapshot-key (w/object target #js {:thread thread})) true) false)))))))))

(defn deliver-whatsapp! [dependencies event key]
  (-> (p/then (p/resolved nil) (fn [_] (whatsapp! dependencies event key)))
      (p/catch (fn [error]
                 (p/let [repaired (repair-topic! dependencies event key error)]
                   (if repaired (whatsapp! dependencies event key)
                       (let [data (api/error-data error)]
                         (if (and (= "telegram" (:service data)) (= 403 (:status data))
                                  (not (w/truthy? (.-forceMaster event))) (not (w/truthy? (.-replay event))))
                           (deliver-whatsapp! dependencies (w/object event #js {:forceMaster true}) key)
                           (throw error)))))))))

(defn- status-topic? [{:keys [store config]} message]
  (let [feed (store/get store "status-feed" (config/digest (js/JSON.stringify #js [(:account config) (:management config)])))]
    (and feed (= (js/String (w/at message "chat" "id")) (.-tgChat feed))
         (= (js/Number (.-message_thread_id message)) (js/Number (.-thread feed))))))

(defn- forward-text! [{:keys [store config api] :as dependencies} bot update message key]
  (let [chat (js/String (w/at message "chat" "id")) private? (= "private" (w/at message "chat" "type"))
        {:keys [route thread contact]} (routing/telegram-context dependencies bot message)]
    (if (and private? (not= bot "master") (not contact))
      (text/send! dependencies bot chat "Connect this bot to a WhatsApp contact in the master bot first, then send your message again." key)
      (p/let [route (if (and private? (not= bot "master") (not route))
                     (conversations/topic! dependencies (.-id contact) (w/first-truthy (.-name contact) (.-id contact)) (str key ":dm-route")) route)]
        (when (and private? (not= bot "master"))
          (let [registered (store/get store "bot" bot)]
            (when-not (w/truthy? (.-ready registered)) (store/put! store "bot" bot (w/object registered #js {:ready true})))))
        (when (and route (not (#{"muted" "ignored"} (.-mode route))))
          (let [reply (when (w/truthy? (.-reply_to_message message)) (store/by-telegram store chat (w/at message "reply_to_message" "message_id") bot))
                mapped (store/by-telegram store chat (.-message_id message) bot)]
            (when (and reply (not= (.-wa_chat reply) (.-chatId route)))
              (throw (api/api-error "Reply belongs to a different WhatsApp conversation" 400 false false 5000)))
            (when-not (and mapped (or (not (w/truthy? (.-edited_message update))) (not= "out" (.-direction mapped))))
              (let [send-key (str "send:" (when private? (str bot ":")) chat ":" (js/String (.-message_id message)))]
                (p/let [decision (-> (mentions/outgoing! dependencies (.-chatId route) message (if (w/truthy? (.-edited_message update)) (str key ":edit-mentions") send-key))
                                    (p/then (fn [outgoing] {:outgoing outgoing}))
                                    (p/catch (fn [error]
                                               (if (mentions/mention-error? error) {:notice (ex-message error)} (throw error)))))]
                  (if-let [notice (:notice decision)]
                    (text/send! dependencies bot chat notice (str key ":mention-error")
                                (w/object (when (w/truthy? thread) #js {:message_thread_id thread})))
                    (let [outgoing (:outgoing decision)]
                    (cond
                      (w/truthy? (.-edited_message update)) (workflow! dependencies :telegram-edit bot update key mapped outgoing)
                      (some #(w/truthy? (aget message %)) ["photo" "video" "animation" "video_note" "audio" "voice" "document" "sticker" "location" "contact" "poll"])
                      (workflow! dependencies :telegram-content bot update key route outgoing)
                      (w/truthy? (.-text outgoing))
                      (p/let [result (effects/run! store send-key
                                                  #((:ow api) (if reply "messages/reply" "messages/send-text")
                                                    #js {:chatId (.-chatId route) :text (.-text outgoing) :quotedMessageId (w/get reply "wa_id") :mentions (.-mentions outgoing)}
                                                    (config/digest send-key)))]
                        (when-not (w/truthy? (.-messageId result))
                          (throw (api/api-error "WhatsApp did not return a message ID; inspect before retrying" 0 false true 5000)))
                        (store/map! store (.-chatId route) (.-messageId result) chat (.-message_id message) bot "out")
                        (ingress/management! dependencies "mark_read" #js {:chatId (.-chatId route)} (str send-key ":read")))
                      :else (text/send! dependencies "master" chat "This Telegram message type cannot be sent to WhatsApp." key
                                       (w/object (when (w/truthy? thread) #js {:message_thread_id thread})))))))))))))))

(defn telegram! [{:keys [store] :as dependencies} bot update key]
  (let [message (w/first-truthy (.-message update) (.-edited_message update))]
    (cond
      (w/truthy? (.-managed_bot update)) (workflow! dependencies :managed-bot (.-managed_bot update) key)
      (and (w/truthy? (w/get message "managed_bot_created")) (= bot "master")) (workflow! dependencies :managed-bot #js {:user (.-from message) :bot (w/at message "managed_bot_created" "bot")} key)
      (w/truthy? (.-my_chat_member update)) (p/resolved js/undefined)
      (w/truthy? (.-callback_query update)) (workflow! dependencies :callback bot (.-callback_query update) key)
      (w/truthy? (.-message_reaction update)) (workflow! dependencies :reaction bot update key)
      (not (w/truthy? message)) (p/resolved js/undefined)
      :else
      (let [text (w/first-truthy (.-text message) (.-caption message) "")
            parsed (controls/command-of message) chat (js/String (w/at message "chat" "id"))]
        (cond
          (and (= bot "master") (= "private" (w/at message "chat" "type")) (not (w/truthy? (.-edited_message update))) (w/truthy? (.-chat_shared message)))
          (workflow! dependencies :group-selection bot message key)
          (or parsed (.startsWith (.trimStart text) "/"))
          (if (or (w/truthy? (.-edited_message update)) (not parsed)) (p/resolved js/undefined) (workflow! dependencies :command bot message key))
          (controls/service-message? message) (p/resolved js/undefined)
          (status-topic? dependencies message)
          (if (w/truthy? (.-edited_message update)) (p/resolved js/undefined)
              (text/send! dependencies bot chat "This topic archives WhatsApp Status posts. Reply to a post with /mute to mute its author’s Status posts, or open the person’s chat to reply." key #js {:message_thread_id (.-message_thread_id message)}))
          (and (w/truthy? (.-edited_message update))
               (.get (.prepare (:db store) "SELECT 1 FROM telegram_messages WHERE scope=? AND chat=? AND message=? AND kind='command'")
                     (routing/scope bot chat) chat (.-message_id message))) (p/resolved js/undefined)
          :else (forward-text! dependencies bot update message key))))))
