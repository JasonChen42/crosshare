# CrossHare design system

Personal photo management, with original-file transfer between Android and desktop.
Approved direction: neutral light surfaces, restrained green accent, compact photo grids,
full-screen media preview, persistent mobile tabs. Functional copy; no fabricated photos or counts.

## Layout

Web: navigation rail, photo workspace, selection toolbar, modal preview. At narrow widths,
navigation wraps above content. Android: photos, albums, transfers, voice tabs; settings in toolbar.
Original voice interface remains accessible while photo functionality is independent.

## Tokens

Canonical web tokens: desktop/internal/server/web/tokens.css.
Typography: system UI / Noto Sans CJK SC, roman headings. Four-point spacing.
Use native Android Material typography and equivalent neutral surfaces and green accent.
No decorative animation; immediate focus ring, visible selected/disabled/error/loading states.

## Semantics

Desktop catalogue presence is not proof that an original remains on the phone.
Delivery completion is a device save acknowledgement, not a continuously verified replica.
No automatic deletion propagation. Albums are logical references, not extra file copies.
