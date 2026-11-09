(ns bifrost.copies
  (:require [bifrost.store :as store]
            [bifrost.config :as config]
            [bifrost.routing :as routing]
            [bifrost.wire :as w]))

(defn scope [{:keys [store config]} chat target]
  (let [destination (js/String (.-chat target))]
    (config/digest (js/JSON.stringify #js [(:account config) (store/canonical store chat) destination
                                          (w/number-or-zero (.-thread target))
                                          (if (.startsWith destination "-") "*" (.-bot target))]))))

(defn target [{:keys [store config]} chat]
  (when-let [route (store/get store "route" chat)]
    (let [bot-id (w/get (store/get store "contact" chat) "bot")
          registered (when (w/truthy? bot-id) (store/get store "bot" bot-id))]
      (if (and (= "dedicated" (.-mode route)) (w/truthy? (w/get registered "ready")))
        #js {:chat (:owner config) :bot bot-id}
        #js {:chat (js/String (.-tgChat route)) :thread (.-thread route)}))))

(defn at? [{:keys [store]} row target]
  (and (= (.-tg_chat row) (js/String (.-chat target)))
       (or (.startsWith (js/String (.-chat target)) "-") (= (.-bot row) (.-bot target)))
       (let [record (.get (.prepare (:db store) "SELECT thread,deleted FROM telegram_messages WHERE scope=? AND chat=? AND message=?")
                          (routing/scope (.-bot row) (.-tg_chat row)) (.-tg_chat row) (.-tg_id row))]
         (or (not record) (and (not (w/truthy? (.-deleted record)))
                              (= (w/number-or-zero (.-thread record)) (w/number-or-zero (.-thread target))))))))

(defn begin! [{:keys [store] :as dependencies} chat id target key]
  (let [scoped (str (scope dependencies chat target) ":" id)
        old (store/get store "destination-start" scoped)]
    (if (and (store/get store "destination-complete" scoped)
             (not (some #(at? dependencies % target) (store/by-whatsapp store chat id))))
      (do (.run (.prepare (:db store) "DELETE FROM kv WHERE kind=? AND id=?") "destination-complete" scoped)
          (store/put! store "destination-start" scoped #js {:key key}) key)
      (do (when-not old (store/put! store "destination-start" scoped #js {:key key}))
          (w/first-truthy (w/get old "key") key)))))

(defn complete! [{:keys [store] :as dependencies} chat id delivered-target]
  (store/put! store "destination-complete" (str (scope dependencies chat delivered-target) ":" id) true)
  (when-let [current (target dependencies chat)]
    (when (= (scope dependencies chat current) (scope dependencies chat delivered-target))
      (store/put! store "wa-complete" (str chat ":" id) true))))
