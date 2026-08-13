# EG25-G module serial research

## Public vendor evidence

- Quectel's EG25-G product page identifies EG25-G as an LTE Cat 4 LGA module and lists the shared `Quectel_EC2x&EG2x&EG9x&EM05_Series_AT_Commands_Manual_V2.2` as its AT command reference: <https://www.quectel.com/product/lte-eg25-g/>.
- A Quectel official support-forum report for EG25-G firmware shows the supported syntax response `+CGSN:"sn,imei"(0,1)`: <https://forums.quectel.com/t/imeisv-read-set-on-eg25-g/57859>. This is evidence to test parameter 0 as module SN and parameter 1 as IMEI; it does not prove the DJI/Baiwang firmware behaves identically.

## Repository evidence

- The scanner reads USB descriptor iSerial into the existing public `serialNumber`, but the currently attached QDC507 exposes no non-empty USB iSerial.
- `internal/modemadapter/qdc507.go` currently uses only `AT+CGSN` for equipment identity and validates it as IMEI. It has no fixed parameter-0 module-SN query.
- Stable device binding is based on an HMAC fingerprint of IMEI, not the display serial, so adding a display SN must not change identity or persistence keys.

## Fixed research probe

For the unique current QDC507, with the Compose Agent stopped so no other process owns the primary AT endpoint, issue exactly:

1. `ATI`
2. `AT+QGMR`
3. `AT+CGSN=?`
4. `AT+CGSN=0`
5. `AT+CGSN=1`

All are read-only. Do not issue `AT+EGMR`, any set form, RF/SIM/SMS/data command, fallback, or retry. Keep raw results only in the private interactive conversation; persist only a bounded availability/shape conclusion.

## Sanitized observation

The one authorized fixed probe completed against exactly one current QDC507. The private firmware reported the documented `sn,imei` parameter grammar; parameter 0 returned one bounded printable module SN, parameter 1 returned a distinct valid IMEI, and both queries terminated normally. Actual manufacturer/model/revision/firmware/SN/IMEI values were shown only in the direct conversation. The Compose Agent was restored healthy and the temporary source/binary was removed.
