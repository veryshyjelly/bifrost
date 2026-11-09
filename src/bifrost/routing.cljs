(ns bifrost.routing
  (:require [bifrost.store :as store]
            [bifrost.config :as config]
            [bifrost.api :as api]
            [bifrost.wire :as w]
            [promesa.core :as p]))

(defn scope [bot chat] (if (.startsWith (js/String chat) "-") "*" bot))

(defn group-context [{:keys [management]} routes message]
  (let [chat (js/String (w/at message "chat" "id"))
        route (some #(when (and (= (js/String (.-tgChat %)) chat)
                                  (not (and (= chat management) (w/truthy? (.-inboxRetired %)) (not (w/truthy? (.-thread %)))))
                                  (or (not (w/truthy? (.-thread %)))
                                      (= (js/Number (.-thread %)) (w/number-or-zero (.-message_thread_id message))))) %) routes)
        thread (cond route (.-thread route)
                     (w/truthy? (.-is_topic_message message)) (.-message_thread_id message)
                     :else js/undefined)]
    {:route route :contact nil :thread thread}))

(defn telegram-context [{:keys [config store]} bot message]
  ;; A private route is keyed by the contact id, even if an old row does not
  ;; contain chatId. Use the original KV key rather than infer identity.
  (if (= "private" (w/at message "chat" "type"))
    (let [contact (some #(when (= (.-bot %) bot) %) (store/all store "contact"))]
      {:route (when contact (store/get store "route" (.-id contact))) :contact contact :thread js/undefined})
    (group-context config (store/all store "route") message)))

(defn token [{:keys [config store]} bot]
  (if (= bot "master") (:masterToken config)
      (if-let [registered (store/get store "bot" bot)]
        (store/unseal store (.-token registered))
        (throw (api/api-error "Unregistered bot" 404 false false 5000)))))

(defn preferred! [{:keys [store api] :as dependencies} sender chat private?]
  (let [contact (store/get store "contact" sender)
        bot-id (w/get contact "bot")
        registered (when (w/truthy? bot-id) (store/get store "bot" bot-id))]
    (cond
      (not registered) (p/resolved "master")
      private? (p/resolved (if (w/truthy? (.-ready registered)) (js/String bot-id) "master"))
      :else (-> (p/let [member ((:tg api) (token dependencies "master") "getChatMember" #js {:chat_id chat :user_id (js/Number bot-id)})]
                  (if (#{"member" "administrator" "creator"} (.-status member)) (js/String bot-id) "master"))
                (p/catch (fn [error] (if (= 400 (:status (api/error-data error))) "master" (throw error))))))))

(defn webhook-secret [{:keys [config]} bot] (config/sign bot (:admin config)))
