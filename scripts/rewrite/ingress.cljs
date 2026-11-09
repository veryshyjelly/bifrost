(ns rewrite-ingress
  (:require ["node:fs" :as fs]
            ["./ingress-driver.ts" :as driver]
            [bifrost.store :as store]
            [bifrost.identity :as identity]
            [bifrost.http :as http]
            [bifrost.routing :as routing]
            [bifrost.controls :as controls]
            [promesa.core :as p]))

(def input (js/JSON.parse (fs/readFileSync 0 "utf8")))
(def database (store/open! ":memory:" (.-key (.-config input)) {:now (fn [] (.-now input))}))
(def configuration (js->clj (.-config input) :keywordize-keys true))
(def account (identity/account-identity! {:get-value #(store/get database %1 %2) :put-value #(store/put! database %1 %2 %3)}
                                       (:account configuration) (:session configuration)))
(def dependencies {:config (assoc configuration :account account) :store database})
(defn optional [value] (if (nil? value) js/undefined value))

(-> (driver/exercise
     #js {:handler (http/handler dependencies)
          :put #(store/put! database %1 %2 %3)
          :sql #(.exec (:db database) %)
          :context (fn [bot message] (let [{:keys [route contact thread]} (routing/telegram-context dependencies bot message)]
                                      #js {:route (optional route) :contact (optional contact) :thread thread}))
          :command controls/command-of
          :snapshot (fn [] (js/Object.fromEntries
                            (into-array (map (fn [table] #js [table (.all (.prepare (:db database) (str "SELECT * FROM " table " ORDER BY rowid")))])
                                             ["kv" "jobs" "effects" "messages" "aliases" "telegram_messages"]))))} input)
    (p/then #(println (js/JSON.stringify %)))
    (p/finally #(store/close! database)))
