(ns rewrite-store
  (:require ["node:fs" :as fs]
            [bifrost.runtime :as runtime]
            [bifrost.store :as store]
            [bifrost.identity :as identity]
            [bifrost.config :as config]))

(runtime/assert-bun!)
(def input (js/JSON.parse (fs/readFileSync 0 "utf8")))
(def clock (.-now input))
(def db (atom nil))
(def tables ["kv" "jobs" "effects" "messages" "aliases" "telegram_messages"])
(defn open! [recover]
  (reset! db (store/open! (.-path input) (.-key input) {:recover recover :now (fn [] clock)})))

(defn snapshot []
  (let [sql (:db @db)]
    (into {} (map (fn [table] [table (.all (.prepare sql (str "SELECT * FROM " table " ORDER BY rowid")))])) tables)))

(defn step [row]
  (let [args (array-seq (.-args row))
        op (.-op row)
        outcome (try
                  #js {:value (case op
                            "put" (do (apply store/put! @db args) true)
                            "get" (apply store/get @db args)
                            "all" (apply store/all @db args)
                            "canonical" (apply store/canonical @db args)
                            "alias" (do (apply store/alias! @db args) true)
                            "enqueue" (apply store/enqueue! @db args)
                            "next" (store/next! @db)
                            "finish" (do (apply store/finish! @db args) true)
                            "fail" (do (apply store/fail! @db args) true)
                            "map" (do (apply store/map! @db args) true)
                            "byTelegram" (apply store/by-telegram @db args)
                            "byWhatsApp" (apply store/by-whatsapp @db args)
                            "seal" (apply store/seal @db args)
                            "unsealDigest" (config/digest (apply store/unseal @db args))
                            "sql" (do (.exec (:db @db) (first args)) true)
                            "schema" (clj->js (mapv (fn [table] {:table table
                                                       :columns (.all (.prepare (:db @db) (str "PRAGMA table_info(" table ")")))
                                                       :indexes (.all (.prepare (:db @db) (str "PRAGMA index_list(" table ")")))}) tables))
                            "reopen" (do (store/close! @db) (open! (first args)) true)
                            "account" (identity/account-identity!
                                       {:get-value #(store/get @db %1 %2)
                                        :put-value #(store/put! @db %1 %2 %3)
                                        :uuid (constantly "synthetic-uuid")}
                                       (first args) (second args)))}
                  (catch :default error #js {:error (.-message error)}))]
    (js/Object.assign outcome #js {:state (clj->js (snapshot))})))

(open! (.-recover input))
(try
  (println (js/JSON.stringify (clj->js (mapv step (.-steps input)))))
  (finally (store/close! @db)))
