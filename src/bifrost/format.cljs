(ns bifrost.format
  (:refer-clojure :exclude [concat])
  (:require [clojure.string :as str]))

(defn plain [text] {:text (js/String text) :entities []})

(defn styled
  ([text type] (styled text type {}))
  ([text type attributes]
   {:text text :entities (if (js/Boolean text)
                          [(merge {:type type :offset 0 :length (.-length text)} attributes)]
                          [])}))

(defn concat [& parts]
  (reduce (fn [{:keys [text entities]} part]
            (let [value (if (string? part) (plain part) part)]
              {:text (str text (:text value))
               :entities (into entities (map #(update % :offset + (.-length text)) (:entities value)))}))
          (plain "") parts))

(defn whatsapp-url [id]
  (str "https://wa.me/" (second (re-find #"^\+?([1-9]\d{4,14})@(?:c\.us|s\.whatsapp\.net)$" id))))

(defn sender-name [name id]
  (let [displayed (-> (js/String name) (str/replace #"[\r\n\t]+" " ") str/trim)
        result (styled displayed "bold")]
    (if (seq displayed)
      (update result :entities conj {:type "text_link" :offset 0 :length (.-length displayed) :url (whatsapp-url id)})
      result)))

(defn- valid-timestamp [timestamp now]
  (let [unix (js/Number timestamp)]
    (when (and (js/Number.isSafeInteger unix) (> unix 0)
               (<= unix (+ (js/Math.floor (/ now 1000)) (* 1098 86400))))
      unix)))

(defn history-time
  ([timestamp] (history-time timestamp "History" (js/Date.now)))
  ([timestamp label now]
   (if-let [unix (valid-timestamp timestamp now)]
     (let [fallback (str (-> (.toISOString (js/Date. (* unix 1000))) (.slice 0 16) (str/replace "T" " ")) " UTC")]
       (concat (styled label "italic") " · "
               (styled fallback "date_time" {:unix_time unix :date_time_format "dt"})))
     (styled label "italic"))))

(defn history-clock
  ([timestamp] (history-clock timestamp "UTC" (js/Date.now)))
  ([timestamp timezone now]
   (if-let [unix (valid-timestamp timestamp now)]
     (let [clock (js/Intl.DateTimeFormat. "en-GB" #js {:timeZone timezone :hour "2-digit" :minute "2-digit" :hourCycle "h23"})]
       (styled (.format clock (js/Date. (* unix 1000))) "date_time" {:unix_time unix :date_time_format "t"}))
     (styled "time unknown" "italic"))))

(defn history-day [timestamp timezone now]
  (if-let [unix (valid-timestamp timestamp now)]
    (let [date (js/Date. (* unix 1000))
          key-clock (js/Intl.DateTimeFormat. "en-CA" #js {:timeZone timezone :year "numeric" :month "2-digit" :day "2-digit"})
          label-clock (js/Intl.DateTimeFormat. "en-GB" #js {:timeZone timezone :year "numeric" :month "long" :day "numeric"})
          fallback (.format label-clock date)]
      {:key (.format key-clock date)
       :label (update (styled fallback "date_time" {:unix_time unix :date_time_format "D"})
                      :entities conj {:type "bold" :offset 0 :length (.-length fallback)})})
    {:key "unknown" :label (styled "Date unknown" "bold")}))

(defn relay-header [options now]
  (let [{:keys [bot sender name own historical status timestamp editedNotice historyStyle timeZone]} options
        timezone (if (contains? options :timeZone) timeZone "UTC")]
    (if (and historical (not status) (= historyStyle "compact"))
      (let [name (cond own (sender-name "You" sender) (= bot "master") (sender-name name sender) :else (plain ""))
            header (concat name (if (seq (:text name)) " [" "[") (history-clock timestamp timezone now) "]")]
        (if editedNotice (concat (styled "Edited" "italic") " · " header) header))
      (let [parts (cond-> []
                    editedNotice (conj (styled "Edited" "italic"))
                    status (conj (history-time timestamp "Status" now))
                    (and (not status) historical) (conj (history-time timestamp "History" now))
                    own (conj (concat (sender-name "You" sender) " · " (styled "sent from WhatsApp" "italic")))
                    (and (not own) (= bot "master")) (conj (sender-name name sender)))]
        (reduce #(if (seq (:text %1)) (concat %1 " · " %2) %2) (plain "") parts)))))

(defn relay-text [options body now]
  (let [header (relay-header options now)
        content (if (string? body) (plain body) body)]
    (cond (empty? (:text header)) content
          (empty? (:text content)) header
          :else (concat header (if (or (:own options) (= "master" (:bot options))
                                      (and (:historical options) (not (:status options)) (= "compact" (:historyStyle options))))
                                 ": " " · ") content))))

(defn slice-formatted [{:keys [text entities]} start end]
  {:text (.slice text start end)
   :entities (into [] (keep (fn [entity]
                             (let [left (max start (:offset entity))
                                   right (min end (+ (:offset entity) (:length entity)))]
                               (when (and (> right left)
                                          (or (not= "date_time" (:type entity))
                                              (and (= left (:offset entity)) (= right (+ (:offset entity) (:length entity))))))
                                 (assoc entity :offset (- left start) :length (- right left)))))) entities)})

(defn- boundary [{:keys [text entities]} start limit]
  (let [end (min (.-length text) (+ start limit))
        end (if (and (< end (.-length text))
                     (<= 0xD800 (.charCodeAt text (dec end)) 0xDBFF)
                     (<= 0xDC00 (.charCodeAt text end) 0xDFFF)) (dec end) end)
        end (reduce (fn [end entity]
                      (if (and (= "date_time" (:type entity)) (< (:offset entity) end)
                               (> (+ (:offset entity) (:length entity)) end)) (:offset entity) end)) end entities)]
    (when (<= end start) (throw (js/Error. "Formatted atomic span exceeds the message limit")))
    end))

(defn split-formatted
  ([value] (split-formatted value 3800))
  ([value limit]
   (loop [start 0 result []]
     (if (< start (.-length (:text value)))
       (let [end (boundary value start limit)]
         (recur end (conj result (slice-formatted value start end))))
       (if (seq result) result [(plain "")])))))

(defn caption-fields [{:keys [text entities]}]
  (when (> (.-length text) 1000) (throw (js/Error. "Caption exceeds the bridge limit")))
  {:caption text :caption_entities entities})
