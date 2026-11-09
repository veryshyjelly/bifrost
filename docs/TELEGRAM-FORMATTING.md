# Telegram formatting

Reviewed against the official Bot API documentation on 2026-11-09.

Ordinary bot messages support bold, italic, underline, strike, spoilers, links, code, blockquotes (including expandable quotes), custom emoji and native dates. Headings, lists and tables belong to the separate RichMessage API. [Telegram HTML reference](https://core.telegram.org/bots/api#html-style).

Equivalent HTML for a master relay (fictional phone number):

```html
<b><a href="https://wa.me/12025550123">Abhishek</a></b>: Hi bro
```

For history, prepend:

```html
<i>History</i> · <tg-time unix="1700000000" format="dt">2023-11-14 22:13 UTC</tg-time> ·
```

`dt` requests a short date and time; `r` requests relative time. Telegram localizes native dates for the viewer. [Date formatting](https://core.telegram.org/bots/api#date-time-entity-formatting).

The bridge sends equivalent entities directly, preserving literal message text and UTF-16 offsets when splitting. [Entity rules](https://core.telegram.org/api/entities).

## Bridge behavior

- Master: bold, linked `Name: body`, with no header newline or generated link preview.
- Member bot: body only; Telegram already displays its identity.
- Incoming WhatsApp mentions: `@Name` with a native `text_mention` for a bound bot present in the destination group, a `t.me` profile link when it is absent, or a WhatsApp link for people without bots. Phone/LID aliases resolve to the same person. Literal number strings without WhatsApp mention metadata are preserved.
- History: native date/time plus the same presentation; UTC fallback text supports older clients.
- Status topic: `Status · native posting time · Name: caption`, always posted by Hermes with the same sender links.
- Media captions, edits, poll titles and unavailable-media notices retain the formatting. Oversized captions are delivered fully as text, with a compact header on the attachment.
- Retries retain their original presentation and text packets. Deliveries already in progress before this upgrade keep their prior plain format.
- Every sender name is linked. A verified phone JID or resolved phone alias opens that person's chat; unknown numbers and opaque LIDs use `https://wa.me/`, the generic WhatsApp launch page. Telegram rejects direct `whatsapp://app` entities. An outgoing private chat ID identifies the other person, so “You” opens its own chat only when the session's number is independently known; otherwise it uses the generic link too.

Links use an international phone number without punctuation, following [WhatsApp click-to-chat](https://faq.whatsapp.com/5913398998672934).

The numberless fallback resolves to WhatsApp's [Open WhatsApp page](https://api.whatsapp.com/). App launching depends on the device and client; it may show an Open/Chat button first.
