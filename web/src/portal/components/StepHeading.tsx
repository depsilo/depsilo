interface Props {
  /** Id of the title, for the step's aria-labelledby. */
  id?: string
  number: number
  title: string
  description?: string
  /**
   * Renders spans instead of a heading, for a step that is itself a
   * disclosure summary (a summary cannot hold a heading).
   */
  inline?: boolean
}

/**
 * A numbered Quick Start step: the mono number in the accent, a title, and
 * one line on what to do. Every pane uses it so step numbers, titles, and
 * descriptions share one column.
 */
export default function StepHeading({ id, number, title, description, inline = false }: Props) {
  const Title = inline ? 'span' : 'h4'
  const Description = inline ? 'span' : 'p'
  return (
    <span data-quickstart-step className={`flex items-start gap-3${inline ? '' : ' mb-3'}`}>
      <span
        aria-hidden="true"
        data-quickstart-step-number
        className="mt-0.5 font-mono text-[12px] font-[600] leading-[1.4] text-[var(--brand-text)]"
      >
        {String(number).padStart(2, '0')}
      </span>
      <span className="block min-w-0">
        <Title id={id} className="m-0 block text-[15px] font-[600] leading-[1.3] text-[var(--text)]">
          {title}
        </Title>
        {description && (
          <Description className="mb-0 mt-1 block text-[13px] leading-[1.5] text-[var(--text-muted)]">
            {description}
          </Description>
        )}
      </span>
    </span>
  )
}
