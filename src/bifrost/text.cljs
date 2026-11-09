(ns bifrost.text
  (:require [bifrost.store :as store]
            [bifrost.effects :as effects]
            [bifrost.format :as format]
            [bifrost.controls :as controls]
            [bifrost.routing :as routing]
            [bifrost.wire :as w]
            [promesa.core :as p]))

(defn- chunks [text]
  (loop [characters (seq (js/Array.from text)) current "" parts []]
    (if-let [character (first characters)]
      (if (> (+ (.-length current) (.-length character)) 3800)
        (recur (next characters) character (conj parts current))
        (recur (next characters) (str current character) parts))
      (if (or (seq current) (empty? parts)) (conj parts current) parts))))

(defn send!
  ([dependencies bot chat text key] (send! dependencies bot chat text key #js {} "log"))
  ([dependencies bot chat text key extra] (send! dependencies bot chat text key extra "log"))
  ([{:keys [store api] :as dependencies} bot chat text key extra kind]
   (let [packets (if (string? text)
                   (into-array (map #(js-obj "text" %) (chunks text)))
                   (or (store/get store "formatted-packets" key)
                       (let [parts (mapv (fn [part]
                                           (let [packet #js {:text (:text part) :entities (clj->js (:entities part))}]
                                             (when (some #(= "text_link" (:type %)) (:entities part))
                                               (aset packet "link_preview_options" #js {:is_disabled true})) packet))
                                         (format/split-formatted text))
                             packets (into-array parts)]
                         (store/put! store "formatted-packets" key packets) packets)))]
     (p/loop [index 0 results []]
       (if (< index (.-length packets))
         (let [packet (aget packets index)
               body (w/object #js {:chat_id chat} extra packet #js {:text (w/first-truthy (.-text packet) "[Empty message]")})]
           (p/let [receipt (effects/run! store (str key ":text:" index) #((:tg api) (routing/token dependencies bot) "sendMessage" body))]
             (if (= kind "chat")
               (controls/track! dependencies bot (if (w/truthy? (.-message_thread_id extra))
                                                  (w/object receipt #js {:message_thread_id (.-message_thread_id extra) :is_topic_message true}) receipt) "chat")
               (controls/reply! dependencies bot receipt extra))
             (p/recur (inc index) (conj results receipt))))
         (into-array results))))))
