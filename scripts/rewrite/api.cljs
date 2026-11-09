(ns rewrite-api
  (:require ["node:fs" :as fs]
            [bifrost.runtime :as runtime]
            [bifrost.api :as api]
            [bifrost.config :as config]
            [promesa.core :as p]))

(runtime/assert-bun!)
(def input (js/JSON.parse (fs/readFileSync 0 "utf8")))

(defn capture-body [body]
  (if (instance? js/FormData body)
    (p/loop [entries (seq (js/Array.from (.entries body))) result {}]
      (if-let [entry (first entries)]
        (let [key (aget entry 0) value (aget entry 1)]
          (if (string? value)
            (p/recur (next entries) (assoc result key value))
            (p/let [bytes (.arrayBuffer value)]
              (p/recur (next entries) (assoc result key {:name (.-name value) :size (.-size value)
                                                        :type (.-type value) :digest (config/digest (js/Buffer.from bytes))})))))
        (clj->js result)))
    (p/resolved body)))

(defn run [row]
  (let [calls (atom [])
        fetch (fn [url init]
                (p/let [body (capture-body (.-body init))]
                  (swap! calls conj #js {:url url :method (.-method init) :headers (.-headers init)
                                         :body body :redirect (.-redirect init) :signal (instance? js/AbortSignal (.-signal init))})
                  (when (.-transport row)
                    (let [error (js/Error. "synthetic transport error")]
                      (aset error "cause" (.-transport row))
                      (throw error)))
                  (let [status (or (.-status row) 200)]
                    #js {:status status :ok (< status 400) :headers (js/Headers. (or (.-headers row) #js {}))
                         :json (fn [] (if (.-invalid row) (p/rejected (js/Error. "synthetic invalid JSON"))
                                         (p/resolved (or (.-response row) #js {:ok true :result #js {:message_id 99}}))))})))
        adapters (api/adapters {:telegram "http://fake-telegram" :openwa "http://fake-openwa" :openwaKey "synthetic-key" :session "session /😀"}
                               {:fetch fetch})
        args (array-seq (.-args row))]
    (-> (p/let [result (case (.-adapter row)
                        "tg" (apply (:tg adapters) args)
                        "ow" (apply (:ow adapters) args)
                        "upload" (apply (:upload adapters) args))]
          #js {:result result :calls (clj->js @calls)})
        (p/catch (fn [error]
                   #js {:error (clj->js (assoc (dissoc (api/error-data error) :api-error) :message (ex-message error)))
                        :calls (clj->js @calls)})))))

(-> (p/loop [rows (seq input) results []]
      (if-let [row (first rows)]
        (p/let [result (run row)] (p/recur (next rows) (conj results result)))
        results))
    (p/then #(println (js/JSON.stringify (clj->js %)))))
