(ns rewrite-relay
  (:require ["node:fs" :as fs]
            ["./relay-driver.ts" :as driver]
            [bifrost.store :as store]
            [bifrost.identity :as identity]
            [bifrost.api :as api]
            [bifrost.ingress :as ingress]
            [bifrost.relay :as relay]
            [bifrost.worker :as worker]
            [bifrost.mentions :as mentions]
            [bifrost.text :as text]
            [promesa.core :as p]))

(def input (js/JSON.parse (fs/readFileSync 0 "utf8")))
(def fixture (driver/providers (fn [error]
                               (if (.-generic error) (js/Error. "synthetic unexpected failure")
                                   (ex-info "synthetic provider refusal"
                                            (merge {:api-error true :status 0 :retry false :uncertain false :delay 5000}
                                                   (js->clj error :keywordize-keys true))))) (.-crash input)))
(def database (atom (store/open! (.-path input) (.-key (.-config input)) {:now (.-now fixture) :recover (true? (.-recover input))})))
(def configuration (js->clj (.-config input) :keywordize-keys true))
(def account (identity/account-identity! {:get-value #(store/get @database %1 %2) :put-value #(store/put! @database %1 %2 %3)}
                                       (:account configuration) (:session configuration)))
(def busy (atom false))
(driver/installCrashHooks (:db @database) (.-crash input))
(defn dependencies [] {:config (assoc configuration :account account) :store @database :busy busy :log-error (.-logError fixture)
                       :api {:tg (.-tg (.-api fixture)) :ow (.-ow (.-api fixture))}})

(-> (driver/exercise
     #js {:put #(store/put! @database %1 %2 %3)
          :sql #(.exec (:db @database) %)
          :map (fn [& args] (apply store/map! @database args))
          :acceptWhatsApp #(ingress/accept-whatsapp! (dependencies) %)
          :acceptTelegram #(ingress/accept-telegram! (dependencies) %1 %2)
          :whatsapp #(relay/whatsapp! (dependencies) %1 %2)
          :deliver #(relay/deliver-whatsapp! (dependencies) %1 %2)
          :telegram #(relay/telegram! (dependencies) %1 %2 %3)
          :tick #(worker/tick! (dependencies))
          :tickConcurrent #(js/Promise.all #js [(worker/tick! (dependencies)) (worker/tick! (dependencies))])
          :sendText (fn [bot chat value key extra kind]
                      (text/send! (dependencies) bot chat (if (string? value) value (js->clj value :keywordize-keys true)) key extra kind))
          :incomingMention (fn [& args] (p/then (apply mentions/incoming! (dependencies) args) clj->js))
          :outgoingMention #(mentions/outgoing! (dependencies) %1 %2 %3)
          :reopen (fn [recover] (store/close! @database)
                    (reset! database (store/open! (.-path input) (:key configuration) {:recover recover :now (.-now fixture)})))
          :error (fn [error] (clj->js (assoc (select-keys (api/error-data error) [:status :retry :uncertain :delay :service :reason]) :message (ex-message error))))
          :snapshot (fn [] (js/Object.fromEntries
                            (into-array (map (fn [table] #js [table (.all (.prepare (:db @database) (str "SELECT * FROM " table " ORDER BY rowid")))])
                                             ["kv" "jobs" "effects" "messages" "aliases" "telegram_messages"]))))} fixture input)
    (p/then #(println (js/JSON.stringify %)))
    (p/finally #(store/close! @database)))
