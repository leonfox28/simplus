# QDC507 CNUM evidence

## Primary contract

- Quectel EC25/EC21 AT Commands Manual V1.3, section 8.1, defines `AT+CNUM`
  as reading the subscriber's own number(s) from the (U)SIM.
- The response may contain zero or more `+CNUM` records followed by a terminal
  result. Type 145 denotes an international number whose value already
  contains `+`; callers must not synthesize the prefix.
- Source: <https://quectel.com/content/uploads/2021/03/Quectel_EC25EC21_AT_Commands_Manual_V1.3.pdf>

## Current-device HIL-0 conclusion

On 2026-08-13, after confirming exactly one QDC507, a ready current SIM,
home registration in CS/PS/EPS domains and the healthy Compose Agent as the
only endpoint owner, a temporary outside-repository probe executed exactly one
fixed read-only `AT+CNUM` query. The response contained exactly one explicit
international E.164 number, type 145, and final `OK`.

The first host attempt was denied before opening the device and therefore sent
no command. The successful attempt used a temporary network-none container
with only the selected tty, a read-only probe bind, all capabilities dropped
and `no-new-privileges`; the normal Agent was restored and health-checked.
The temporary source/binary were deleted. The actual number and raw transcript
were displayed only in the direct private conversation and are intentionally
absent here, from fixtures, logs, and Git.

## Product implication

The current SIM and QDC507 firmware can supply a cellular subscriber-number
observation. The missing Line number is an integration/ownership defect, not
evidence that this SIM or cellular registration lacks a number. Production
must still treat `CNUM` as best effort because the standard permits an empty or
multi-record result and SIM phonebook data may differ from an IMS identity.
