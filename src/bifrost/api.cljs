(ns bifrost.api
  (:require ["node:fs" :as fs]
            [clojure.string :as str]
            [promesa.core :as p]))

(defn api-error
  ([message] (api-error message 0 false false 5000))
  ([message status retry uncertain delay]
   (ex-info message {:api-error true :status status :retry retry :uncertain uncertain :delay delay})))

(defn error-data [error]
  ;; SCI adds source context when an interpreted function throws. The original
  ;; classification remains on its cause, including across Promise boundaries.
  (loop [error error]
    (let [data (ex-data error)]
      (if (:api-error data) data
          (when-let [cause (ex-cause error)] (recur cause))))))

(defn- transport-error [error mutation]
  (let [error (loop [error error]
                (if (and (= :sci/error (:type (ex-data error))) (ex-cause error))
                  (recur (ex-cause error)) error))
        cause (some-> error .-cause)
        errors (some-> cause .-errors)
        codes (if (js/Boolean errors) (map #(.-code %) errors) [(some-> cause .-code)])
        not-connected (if (seq codes) (every? #{"ECONNREFUSED" "ENOTFOUND" "EAI_AGAIN"} codes) 0)]
    (api-error (if (js/Boolean not-connected) "Remote service is not reachable yet" "Remote request failed; inspect delivery outcome")
               0 (if mutation not-connected true) (and mutation (not (js/Boolean not-connected))) 5000)))

(defn- refused-error [response body mutation]
  (let [status (.-status response)
        code (if (>= status 400) status (js/Number (if (js/Boolean (.-error_code body)) (.-error_code body) 500)))
        uncertain (or (= "IDEMPOTENCY_OUTCOME_UNKNOWN" (.-code body)) (and mutation (>= code 500) (not= code 501)))
        retry (or (= code 429) (and (not mutation) (>= code 500)))
        retry-after (some-> body .-parameters .-retry_after)
        header (.get (.-headers response) "retry-after")
        delay (* 1000 (js/Number (cond (js/Boolean retry-after) retry-after (js/Boolean header) header :else 5)))
        description (str/lower-case (js/String (if (js/Boolean (.-description body)) (.-description body) "")))
        reason (cond (str/includes? description "message thread not found") "thread_missing"
                     (re-find #"repl(?:y|ied).*not found" description) "reply_missing"
                     (re-find #"message.*not found" description) "message_missing")
        message (str "API refused request (" code (when (js/Boolean (.-code body)) (str ":" (.-code body))) ")"
                     (when reason (str " [" reason "]")))]
    (ex-info message (cond-> {:api-error true :status code :retry retry :uncertain uncertain :delay delay}
                      reason (assoc :reason reason)))))

(defn request!
  [{:keys [fetch timeout] :or {fetch js/fetch timeout #(js/AbortSignal.timeout 120000)}} url init mutation]
  (p/let [response (-> (p/let [response (fetch url (js/Object.assign #js {} init #js {:redirect "error" :signal (timeout)}))] response)
                      (p/catch #(throw (transport-error % mutation))))
          body (-> (p/let [body (.json response)] body)
                   (p/catch (fn [_] (throw (api-error "Invalid API response" (.-status response) (not mutation) mutation 5000)))))]
    (when (or (not (.-ok response)) (false? (.-ok body)))
      (throw (refused-error response body mutation)))
    body))

(def telegram-reads
  #{"getMe" "getChat" "getChatMember" "getFile" "getWebhookInfo" "getManagedBotToken"
    "getManagedBotAccessSettings" "getMyDescription" "getUserProfilePhotos" "getMyCommands"
    "getChatMenuButton" "getCustomEmojiStickers"})

(defn- service-error [error service]
  (if-let [data (error-data error)] (ex-info (ex-message error) (assoc data :service service)) error))

(defn adapters
  ([config] (adapters config {}))
  ([config dependencies]
   (let [request (partial request! dependencies)
         telegram (fn [token method init mutation]
                    (-> (p/let [response (request (str (:telegram config) "/bot" token "/" method) init mutation)] (.-result response))
                        (p/catch #(throw (service-error % "telegram")))))]
     {:request request
      :tg (fn
            ([token method] (telegram token method #js {:method "POST" :headers #js {"content-type" "application/json"} :body "{}"}
                                      (not (contains? telegram-reads method))))
            ([token method body]
             (telegram token method #js {:method "POST" :headers #js {"content-type" "application/json"} :body (js/JSON.stringify (if (identical? body js/undefined) #js {} body))}
                       (not (contains? telegram-reads method)))))
      :upload (fn [token method field filename name body]
                (p/let [form (js/FormData.)
                        _ (doseq [entry (js/Object.entries body)]
                            (let [key (aget entry 0) value (aget entry 1)]
                              (when-not (identical? value js/undefined)
                                (.set form key (if (or (nil? value) (object? value) (array? value)) (js/JSON.stringify value) (js/String value))))))
                        blob ((or (:open-as-blob dependencies) fs/openAsBlob) filename)]
                  (.set form field blob name)
                  (telegram token method #js {:method "POST" :body form} true)))
      :ow (fn ow
            ([path] (ow path js/undefined js/undefined "POST"))
            ([path body] (ow path body js/undefined "POST"))
            ([path body key] (ow path body key "POST"))
            ([path body key method]
             (let [has-body (not (identical? body js/undefined))
                   headers #js {"X-API-Key" (:openwaKey config) "content-type" "application/json"}
                   init #js {:method (if has-body method "GET") :headers headers}]
               (when (js/Boolean key) (aset headers "Idempotency-Key" key))
               (when has-body (aset init "body" (js/JSON.stringify body)))
               (-> (request (str (:openwa config) "/api/sessions/" (js/encodeURIComponent (:session config)) "/" path) init has-body)
                   (p/catch #(throw (service-error % "openwa")))))))})))
