(ns rewrite-effects
  (:require ["node:fs" :as fs]
            [bifrost.store :as store]
            [bifrost.api :as api]
            [bifrost.effects :as effects]
            [promesa.core :as p]))

(def input (js/JSON.parse (fs/readFileSync 0 "utf8")))
(def db (store/open! ":memory:" (.repeat "aa" 32)))
(def calls (atom 0))
(defn run [row]
  (let [action (fn []
                 (swap! calls inc)
                 (cond
                   (.-apiError row) (let [e (js->clj (.-apiError row) :keywordize-keys true)]
                                         (p/rejected (api/api-error "synthetic refusal" (:status e) (:retry e) (:uncertain e) 5000)))
                   (.-genericError row) (p/rejected (js/Error. "synthetic unexpected error"))
                   :else (p/resolved (.-result row))))]
    (-> (effects/run! db (.-id row) action)
        (p/then (fn [result] #js {:result result}))
        (p/catch (fn [error] #js {:error (clj->js (assoc (select-keys (api/error-data error) [:status :retry :uncertain :delay]) :message (ex-message error)))}))
        (p/then (fn [result] (js/Object.assign result #js {:calls @calls :effects (.all (.prepare (:db db) "SELECT * FROM effects ORDER BY rowid"))}))))))

(-> (p/loop [rows (seq input) results []]
      (if-let [row (first rows)] (p/let [result (run row)] (p/recur (next rows) (conj results result))) results))
    (p/then #(println (js/JSON.stringify (clj->js %))))
    (p/finally #(store/close! db)))
