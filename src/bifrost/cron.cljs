(ns bifrost.cron
  (:require [clojure.string :as str]))

(defn- invalid-field! [] (throw (js/Error. "Invalid cron field")))

(defn- field [text minimum maximum]
  (reduce (fn [values item]
            (when-not (re-matches #"(?:\*|\d+(?:-\d+)?)(?:/\d+)?" item) (invalid-field!))
            (let [[base step-text] (str/split item #"/")
                  step (js/Number (or step-text 1))
                  [start end] (cond (= base "*") [minimum maximum]
                                    (re-matches #"\d+" base) [(js/Number base) (if step-text maximum (js/Number base))]
                                    :else (mapv js/Number (str/split base #"-")))]
              (when (or (< step 1) (> start end)
                        (some #(or (not (js/Number.isInteger %)) (< % minimum) (> % maximum)) [start end]))
                (invalid-field!))
              (into values (range start (inc end) step))))
          #{} (.split text ",")))

(defn schedule [expression timezone]
  (if (= expression "off")
    (constantly false)
    (let [parts (str/split (str/trim expression) #"\s+")]
      (when-not (= 5 (count parts)) (throw (js/Error. "SYNC_CRON must contain five cron fields")))
      (let [[minute hour day month weekday] (mapv field parts [0 0 1 1 0] [59 23 31 12 7])
            weekday (cond-> weekday (contains? weekday 7) (conj 0))
            clock (js/Intl.DateTimeFormat. "en-US" #js {:timeZone timezone :minute "2-digit" :hour "2-digit"
                                                       :day "2-digit" :month "2-digit" :weekday "short" :hourCycle "h23"})]
        (fn [date]
          (let [p (into {} (map (fn [v] [(.-type v) (.-value v)]) (.formatToParts clock date)))
                d (contains? day (js/Number (get p "day")))
                w (contains? weekday (.indexOf #js ["Sun" "Mon" "Tue" "Wed" "Thu" "Fri" "Sat"] (get p "weekday")))
                calendar (if (and (not= "*" (nth parts 2)) (not= "*" (nth parts 4))) (or d w) (and d w))]
            (and (contains? minute (js/Number (get p "minute")))
                 (contains? hour (js/Number (get p "hour")))
                 (contains? month (js/Number (get p "month"))) calendar)))))))
