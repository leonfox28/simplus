# Component Guidelines

> How the current React and Ant Design Pro interface is composed.

## Page Shape

Management screens default-export function components whose root is a
`PageContainer`. They compose Ant Design Pro primitives rather than maintaining
a project-specific design-system wrapper:

```tsx
// web/src/pages/Dashboard.tsx
export default function Dashboard() {
  const screens = Grid.useBreakpoint()
  const compact = !screens.md
  const [health, setHealth] = useState<HealthResponse>()
  const [topology, setTopology] = useState<HardwareTopologyResponse>()
  const [error, setError] = useState('')
  useEffect(() => { Promise.all([getSystemHealth(), getHardwareTopology()]).then(([h,t]) => { setHealth(h); setTopology(t) }).catch(e => setError(String(e))) }, [])
  return <PageContainer title="概览" subTitle="查看系统、硬件和线路的当前运行概况">
    {error && <Alert type="error" message={error} />}
    {!health ? <Spin /> : <StatisticCard.Group direction={compact ? 'column' : 'row'}>
      <StatisticCard statistic={{ title: '系统状态', value: health.status }} />
      <StatisticCard statistic={{ title: '后端', value: health.backend }} />
      <StatisticCard statistic={{ title: '模组', value: topology?.devices.length ?? 0 }} />
      <StatisticCard statistic={{ title: '线路', value: topology?.lines.length ?? 0 }} />
    </StatisticCard.Group>}
  </PageContainer>
}
```

The same composition appears in `web/src/pages/Lines.tsx` and
`web/src/pages/Modems.tsx`. `Login.tsx` and `Setup.tsx` are deliberate
exceptions: their routes set `layout: false`, and each supplies its own full
page shell.

`docs/decisions/0009-ant-design-pro-web.md` explicitly prefers Pro tables,
forms, descriptions, and layout components for new management UI. Current
examples use `ProTable`, `ProForm`/`ModalForm`, `ProDescriptions`, `ProCard`,
and `StatisticCard`.

## Local Components and Props

Small view-only components remain beside their sole consumer. Derive prop types
from the API model instead of duplicating its fields:

```tsx
// web/src/pages/Modems.tsx
function SIMPresenceTag({ value }: { value: ManagedModem['simPresence'] }) {
  if (value === 'present') return <Tag color="green">已插入</Tag>
  if (value === 'absent') return <Tag>未插入</Tag>
  return <Tag color="orange">未知</Tag>
}

function ModemModel({ value, strong = false }: { value: string, strong?: boolean }) {
  if (!value) return <Typography.Text type="danger">读取失败</Typography.Text>
  return <Typography.Text strong={strong}>{value}</Typography.Text>
}
```

`CapabilityTags` in the same file and `subscriptionStatus` in
`web/src/pages/Mihomo.tsx` follow this local-helper pattern. Page-specific
composite rows and form shapes use nearby aliases such as `LineRow`,
`CreateSubscriptionValues`, and `EditSubscriptionValues`.

## Tables, Forms, and Actions

- Give tables the domain type (`ProTable<ManagedModem>`,
  `ProTable<MihomoSubscription>`, or `ProColumns<LineRow>[]`) and a stable
  business `rowKey`, normally `id` or `candidateId`.
- Management tables normally set `search={false}` and
  `scroll={{ x: 'max-content' }}`. `Lines.tsx`, `Modems.tsx`,
  `Messages.tsx`, and `Mihomo.tsx` demonstrate this contract.
- Keep API field values as the table data source and translate domain states at
  render time through exhaustive label maps or helpers. Examples include
  `lineStateLabels` in `Lines.tsx`, `readinessLabels` in `Modems.tsx`, and
  `smsStatusPresentation` in `messages/status.ts`.
- Use ProForm rules at the input boundary and await mutations before returning
  `true` from `onFinish`. See the URL rules in `Mihomo.tsx`, absolute-path rule
  in `Setup.tsx`, and password confirmation rule in `Settings.tsx`.
- Put a confirmation boundary around destructive actions. Subscription,
  notification-channel, and message deletion use `Popconfirm` in
  `Mihomo.tsx`, `Notifications.tsx`, and `Messages.tsx`.
- Represent loading, empty, unavailable, and failed states explicitly with
  `Spin`, `Empty`, disabled controls, `Tag`, or `Alert`. `Dashboard.tsx`,
  `Modems.tsx`, and `Lines.tsx` contain representative cases.

The UI awaits server confirmation rather than presenting mutations as already
successful. `Modems.tsx` updates RF state from the returned `ManagedModem`, and
`Lines.tsx` reloads related resources after Line or egress changes.

## Responsive Layout and Styling

Responsiveness is implemented with Ant Design primitives and a small global CSS
layer:

```tsx
// web/src/pages/Mihomo.tsx
const screens = Grid.useBreakpoint()
const compact = !screens.md
```

- Wide tables scroll locally with `scroll={{ x: 'max-content' }}`.
- Inline forms switch to vertical layout on compact screens in `Calls.tsx` and
  `Notifications.tsx`.
- Reusable global rules live in `web/src/global.css`: `.page-grid`, `.mono`,
  login layout, and ProLayout mobile overflow fixes.
- Component-specific spacing and one-off grids currently use Ant Design props
  and inline `style` objects, as in `Mihomo.tsx` and `Modems.tsx`; there are no
  CSS Modules, styled-components, or Tailwind classes.

## Accessibility and Sensitive Values

Use the semantic behavior already present in the component library, then add a
label when an icon or state alone would be ambiguous:

- Login fields have stable native `id`, `name`, `type`, and `autoComplete`
  attributes in `Login.tsx`; `Login.test.tsx` asserts them.
- IMEI reveal and RF controls have `aria-label` values in `Modems.tsx`.
  `Modems.test.tsx` exercises their row-specific behavior through test IDs and
  also asserts the RF switch's accessible name by role.
- Candidate radio controls expose their disabled reason through `aria-label` in
  `Lines.tsx`; `Lines.test.tsx` verifies selectable and disabled states.
- Table deletion controls in `Mihomo.tsx`, `Notifications.tsx`, and
  `Messages.tsx` are paired with `Popconfirm`. Closable contact tags in
  `Messages.tsx` are an existing immediate-delete exception; do not describe
  confirmation as a universal current behavior.

Sensitive data follows the product boundary, not generic table convenience.
`Modems.tsx` masks IMEI, fetches it only after an explicit click, clears all
revealed values on reload, and never puts it in the ordinary Modem list.

## Product Vocabulary

`docs/architecture.md` requires the Web UI to use business terms (Modem, SIM,
Line, Message, Call) and not expose Agent protocols, AT commands, sysfs or
`/dev` paths, or internal fencing. The Modem add dialog may show only the
bounded identification metadata documented there. `api/client.test.ts` rejects
raw paths and identity fingerprints at the boundary, while `Modems.test.tsx`
and `Lines.test.tsx` exercise the visible managed/candidate surfaces.

Most page copy is written directly in Chinese JSX. The locale dictionaries in
`web/src/locales/en-US.ts` and `zh-CN.ts` currently translate menu keys only;
the application does not yet have a general page-copy internationalization
layer.

## Current Formatting Reality

Recently expanded pages (`Lines.tsx`, `Modems.tsx`, `Mihomo.tsx`) use readable
multi-line JSX and extracted helpers. Several older/smaller pages
(`Calls.tsx`, `Notifications.tsx`, `Settings.tsx`, and parts of `Messages.tsx`)
remain densely formatted. There is no frontend formatter task. Preserve local
style in a focused edit and do not perform an unrelated whole-file reformat.
