(ns bifrost.mentions
  (:require [clojure.string :as str]
            [bifrost.store :as store]
            [bifrost.config :as config]
            [bifrost.routing :as routing]
            [bifrost.api :as api]
            [bifrost.format :as format]
            [bifrost.wire :as w]
            [promesa.core :as p]))

(defn- mention-error [message] (ex-info message {:mention-error true}))
(defn mention-error? [error]
  (loop [error error]
    (if (:mention-error (ex-data error)) true (when-let [cause (ex-cause error)] (recur cause)))))
(defn session [{:keys [config]}] (config/digest (:account config)))
(defn canonical [{:keys [store]} id] (store/canonical store (.replace id #":\d+@" "@")))
(defn- local [id] (aget (.split (aget (.split id "@") 0) ":") 0))
(defn- person? [id] (and (string? id) (boolean (re-matches #"\d+(?::\d+)?@(?:c\.us|s\.whatsapp\.net|lid)" id))))
(defn- clean-name [name]
  (.join (.slice (js/Array.from (.trim (.replace (js/String name) (js/RegExp. "[{}|\\r\\n\\t\\x00-\\x1f]" "g") " "))) 0 48) ""))
(defn- contact [{:keys [store] :as dependencies} id]
  (some #(when (= (canonical dependencies (.-id %)) (canonical dependencies id)) %) (store/all store "contact")))
(defn- cached [{:keys [store] :as dependencies} chat] (store/get store "sync-group" (str (session dependencies) ":" chat)))
(defn- name-for [dependencies id chat]
  (let [member (some #(when (= (canonical dependencies (.-id %)) (canonical dependencies id)) %) (w/get (cached dependencies chat) "members"))]
    (clean-name (w/first-truthy (w/get (contact dependencies id) "name") (w/get member "name") (local id)))))

(defn roster! [{:keys [store api] :as dependencies} chat]
  (let [old (cached dependencies chat)]
    (if (and old (< (- ((:now store)) (.-fetchedAt old)) 300000))
      (p/resolved (w/object old #js {:cached false}))
      (-> (p/let [info ((:ow api) (str "groups/" (js/encodeURIComponent chat)))]
            (when (or (not (array? (.-participants info))) (not (string? (.-name info))) (empty? (.trim (.-name info)))
                      (some #(not (person? (.-id %))) (.-participants info)))
              (throw (api/api-error "OpenWA group member list is incomplete" 502 false false 5000)))
            (let [members (.map (.-participants info)
                                (fn [member] #js {:id (.-id member) :canonical (canonical dependencies (.-id member))
                                                  :name (w/first-truthy (.-name member) "") :isAdmin (w/truthy? (.-isAdmin member))
                                                  :isSuperAdmin (w/truthy? (.-isSuperAdmin member))}))
                  roster-hash (config/digest (js/JSON.stringify (.sort (.map members #(str (.-canonical %) ":" (.-isAdmin %) ":" (.-isSuperAdmin %))))))
                  result (w/object old #js {:name (.-name info) :description (.-description info) :members members
                                            :rosterHash roster-hash :fetchedAt ((:now store))})]
              (store/put! store "sync-group" (str (session dependencies) ":" chat) result)
              (w/object result #js {:cached false})))
          (p/catch (fn [_] (throw (mention-error "Could not refresh the WhatsApp member list. Try /sync, then send your message again."))))))))

(defn- incoming-tag! [{:keys [store api] :as dependencies} id chat destination]
  (let [contact (contact dependencies id) bot-id (w/get contact "bot")
        bot (when (w/truthy? bot-id) (store/get store "bot" bot-id)) label (str "@" (name-for dependencies id chat))]
    (p/let [member (if (and bot (.startsWith destination "-"))
                    (-> (p/let [response ((:tg api) (routing/token dependencies "master") "getChatMember"
                                          #js {:chat_id destination :user_id (js/Number bot-id)})]
                          (boolean (#{"member" "administrator" "creator"} (.-status response))))
                        (p/catch (fn [error] (if (#{400 403} (:status (api/error-data error))) false (throw error)))))
                    false)]
      (if member
        (format/styled label "text_mention" {:user (cond-> {:id (js/Number bot-id) :is_bot true :first_name (clean-name (w/first-truthy (w/get contact "name") label))}
                                                   (w/truthy? (.-username bot)) (assoc :username (.-username bot)))})
        (format/styled label "text_link" {:url (if (w/truthy? (w/get bot "username")) (str "https://t.me/" (.-username bot)) (format/whatsapp-url id))})))))

(defn incoming! [{:keys [store] :as dependencies} body ids chat destination key]
  (let [body (js/String (w/first-truthy body ""))
        cache-key (config/digest (js/JSON.stringify #js [(session dependencies) key body (w/first-truthy ids #js [])]))
        saved (store/get store "mention-render" cache-key)]
    (if saved (p/resolved (js->clj saved :keywordize-keys true))
        (let [targets (reduce (fn [targets id]
                                (let [resolved (canonical dependencies id)
                                      aliases (.all (.prepare (:db store) "SELECT alias FROM aliases WHERE canonical=?") resolved)
                                      candidates (concat [id resolved] (map #(js/String (.-alias %)) aliases))]
                                  (reduce #(update %1 (local %2) (fnil conj #{}) resolved) targets candidates)))
                              {} (filter person? (if (array? ids) ids [])))
              matches (seq (js/Array.from (.matchAll body (js/RegExp. "(?<![\\p{L}\\p{N}_@])@(\\d+)(?![\\p{L}\\p{N}_])" "gu"))))]
          (p/loop [matches matches cursor 0 result (format/plain "") rendered {}]
            (if-let [match (first matches)]
              (let [people (get targets (aget match 1))]
                (if (not= 1 (count people)) (p/recur (next matches) cursor result rendered)
                    (let [id (first people)]
                      (p/let [tag (or (get rendered id) (incoming-tag! dependencies id chat destination))]
                        (p/recur (next matches) (+ (.-index match) (.-length (aget match 0)))
                                 (format/concat result (.slice body cursor (.-index match)) tag) (assoc rendered id tag))))))
              (let [result (format/concat result (.slice body cursor))]
                (store/put! store "mention-render" cache-key (clj->js result)) result)))))))

(defn- add-span [dependencies spans start end id]
  (if (some #(and (< (:start %) end) (> (:end %) start)) spans) spans
      (conj spans {:start start :end end :id (canonical dependencies id)})))

(defn- resolve-outgoing! [{:keys [store] :as dependencies} chat message key]
  (let [cache-key (config/digest (js/JSON.stringify #js [(session dependencies) key]))
        previous (store/get store "outbound-mentions" cache-key)]
    (if previous (p/resolved previous)
        (let [text (js/String (if (nil? (.-text message)) (if (nil? (.-caption message)) "" (.-caption message)) (.-text message)))
              entities (w/first-truthy (if (identical? (.-text message) js/undefined) (.-caption_entities message) (.-entities message)) #js [])
              excluded? (fn [start end] (some #(and (#{"code" "pre" "url" "text_link" "email"} (.-type %))
                                                   (< (.-offset %) end) (> (+ (.-offset %) (.-length %)) start)) entities))
              for-bot (fn [id] (some #(when (= (js/String (.-bot %)) id) %) (store/all store "contact")))
              spans (reduce (fn [spans entity]
                              (let [id (w/at entity "user" "id") contact (when (w/truthy? id) (for-bot (js/String id)))]
                                (if (and (= "text_mention" (.-type entity)) contact (store/get store "bot" (js/String id)))
                                  (add-span dependencies spans (.-offset entity) (+ (.-offset entity) (.-length entity)) (.-id contact)) spans))) [] entities)
              spans (reduce (fn [spans match]
                              (let [start (.-index match) end (+ start (.-length (aget match 0)))]
                                (if (excluded? start end) spans
                                    (let [id-match (.exec #"\|([a-f0-9]{8,64})\}$" (aget match 0))
                                          token (when id-match (store/get store "mention-token" (aget id-match 1)))]
                                      (when (or (not token) (not= (.-session token) (session dependencies)) (not= (.-chat token) chat))
                                        (throw (mention-error "This mention tag is unknown or belongs to another group/account. Use /mention to copy a new tag, then resend.")))
                                      (add-span dependencies spans start end (.-person token))))))
                            spans (js/Array.from (.matchAll text (js/RegExp. "@\\{[^{}\\n]*(?:\\}|(?=\\n|$))" "g"))))
              spans (reduce (fn [spans match]
                              (let [start (.-index match) end (+ start (.-length (aget match 0)))
                                    bot (some #(when (= (some-> % .-username .toLowerCase) (.toLowerCase (aget match 1))) %) (store/all store "bot"))
                                    contact (when bot (for-bot (.-id bot)))]
                                (if (or (excluded? start end) (not contact)) spans
                                    (add-span dependencies spans start end (.-id contact)))))
                            spans (js/Array.from (.matchAll text (js/RegExp. "(?<![\\p{L}\\p{N}_@/])@([a-zA-Z0-9_]+)(?![\\p{L}\\p{N}_])" "gu"))))]
          (p/let [roster (when (and (seq spans) (.endsWith chat "@g.us")) (roster! dependencies chat))]
            (when (and roster (some (fn [span] (not (some #(= (canonical dependencies (.-id %)) (:id span)) (.-members roster)))) spans))
              (throw (mention-error "A tagged person is no longer in this WhatsApp group. Use /mention to find a current member, then resend.")))
            (let [{:keys [converted cursor ids]} (reduce (fn [{:keys [converted cursor ids]} span]
                                                         {:converted (str converted (.slice text cursor (:start span)) "@" (local (:id span)))
                                                          :cursor (:end span) :ids (if (some #{(:id span)} ids) ids (conj ids (:id span)))})
                                                       {:converted "" :cursor 0 :ids []} (sort-by :start spans))
                  result #js {:text (str converted (.slice text cursor)) :mentions (into-array ids)}]
              (store/put! store "outbound-mentions" cache-key result) result))))))

(defn outgoing! [dependencies chat message key]
  ;; Match an async function's rejection semantics even when validation fails
  ;; before roster I/O. Relay catches these failures to send a scoped notice.
  (p/then (p/resolved nil) (fn [_] (resolve-outgoing! dependencies chat message key))))
