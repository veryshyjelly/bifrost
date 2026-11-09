(ns bifrost.conversations
  (:require [bifrost.store :as store]
            [bifrost.effects :as effects]
            [bifrost.ingress :as ingress]
            [bifrost.routing :as routing]
            [bifrost.api :as api]
            [bifrost.wire :as w]
            [promesa.core :as p]))

(defn topic! [{:keys [store config api] :as dependencies} chat-id name key]
  (if-let [route (store/get store "route" chat-id)] (p/resolved route)
      (let [bot-id (w/get (store/get store "contact" chat-id) "bot")
            bot (when (w/truthy? bot-id) (store/get store "bot" bot-id))
            private? (not (.endsWith chat-id "@g.us"))]
        (if (and private? (w/truthy? (w/get bot "ready")))
          (let [route #js {:chatId chat-id :name name :tgChat (:management config) :mode "dedicated" :history false :inboxRetired true}]
            (store/put! store "route" chat-id route) (p/resolved route))
          (if (not (w/truthy? (:management config)))
            (p/rejected (api/api-error "Management forum group has not been configured" 409 true false 5000))
            (p/let [topic (effects/run! store (str key ":topic")
                                        #((:tg api) (routing/token dependencies "master") "createForumTopic"
                                          #js {:chat_id (:management config) :name (w/first-truthy (.slice name 0 128) chat-id)}))]
              (let [route #js {:chatId chat-id :name name :tgChat (:management config) :thread (.-message_thread_id topic)
                               :mode (if (and private? (w/truthy? (w/get (store/get store "contact" chat-id) "bot"))) "dedicated" "inbox") :history false}]
                (store/put! store "route" chat-id route)
                (ingress/management! dependencies "new_conversation" #js {:chatId chat-id} (str "new:" chat-id)) route)))))))
