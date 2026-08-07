# Component Guidelines

> How the React and direct Ant Design interface is composed.

## Direct Ant Design, Not Ant Design Pro

“Use Ant Design” means importing the base library's components directly:

```tsx
import { Alert, Button, Card, Form, Table } from 'antd'
```

It does not include `@ant-design/pro-components`, `ProTable`, `ProForm`,
`PageContainer`, or ProLayout. Those higher-level Pro abstractions supplied
many built-in features but also pulled the previous Umi-centric runtime into
the application. Current pages deliberately compose only the primitives they
need.

## Shell and Page Shape

`AppShell` owns the authenticated chrome. It uses an Ant Design `Layout.Sider`
on wide screens and a `Drawer` on compact screens; pages render through React
Router's `Outlet`. Pages use product-owned primitives from
`web/src/components/Page.tsx`:

```tsx
export default function ExamplePage() {
  return <main className="page-content">
    <PageHeader title="标题" subtitle="说明" />
    <PageSection title="当前状态">
      <AsyncState loading={query.isPending} error={query.error}>
        {/* Ant Design content */}
      </AsyncState>
    </PageSection>
  </main>
}
```

- `PageHeader` provides title/subtitle/actions.
- `PageSection` provides a consistent `Card` boundary.
- `AsyncState` renders loading, error/retry, empty, or content states.
- `ResponsiveDataView` renders a `Table` at `md` and record cards below it.

These are thin project primitives, not a replacement design system.

## Tables, Cards, Forms, and Actions

- Give tables `TableColumnsType<DomainType>` and a stable business `rowKey`.
- Do not squeeze a wide table onto mobile. Supply a card renderer containing
  the important fields and actions; preserve local horizontal scroll as a
  last-resort desktop containment boundary.
- Type forms (`Form<Values>`, `Form.useForm<Values>()`) and mirror simple input
  constraints for immediate feedback. Generated Zod/server validation remains
  authoritative.
- Await or observe mutation completion before reporting success. Display
  mutation errors with `displayApiError`; keep the previous confirmed snapshot
  visible on failure.
- Use `Popconfirm` or a modal confirmation for destructive actions.
- Show loading, empty, disabled/unavailable, partial-failure, and retry states
  explicitly. A disabled hardware action needs visible context, not silence.

## Responsive and Accessibility Contract

- Use `Grid.useBreakpoint()` for behavior that genuinely changes by viewport;
  use `global.css` for layout, spacing, overflow containment, and card stacks.
- The desktop shell must not create page-level horizontal overflow. The mobile
  shell uses a Drawer with `autoFocus={false}` so merely opening navigation
  does not focus an unrelated input.
- Prefer roles, labels, visible text, and native input attributes. Icon-only
  buttons require an `aria-label`.
- Login/password inputs preserve stable `name`, `type`, and `autoComplete`.
- Keep focusable actions keyboard accessible; do not implement clickable
  `div`s for controls already represented by Ant Design buttons/menus.

## Sensitive and Product Data

Render product terms (Modem, SIM/Profile, Line, Message, Call), not Agent
protocols, AT/QMI commands, device paths, or fencing details. IMEI remains
masked and transient: it is fetched only after explicit intent and cleared on
hide/reload. Subscription lists render the bounded URL hint rather than the
full credential-bearing URL; the complete value is shown only inside the
explicit edit flow. SSE attention text must never include message content,
numbers, or hardware identity.

## Wrong vs Correct

```tsx
// Wrong: framework-heavy table and one layout for every viewport
<ProTable<SmsMessage> request={loadMessages} />

// Correct: generated query state plus explicit desktop/mobile presentation
<ResponsiveDataView<SmsMessage>
  data={messages}
  columns={columns}
  rowKey="id"
  renderCard={(record) => <MessageCard message={record} />}
/>
```

## Avoid

- Reintroducing Pro Components to save a few lines on one screen.
- A generic wrapper for every Ant Design component.
- Optimistic success for RF, SMS, call, or other uncertain operations.
- Hiding errors in console output or relying on raw browser network messages.
- Whole-file style rewrites unrelated to the requested behavior.
