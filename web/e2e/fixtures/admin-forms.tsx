import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import InputV2 from '../../src/components/Input'
import SelectV2 from '../../src/components/Select'
import TextareaV2 from '../../src/components/Textarea'
import '../../src/index.css'

const fieldKinds = [
  { name: 'input', Component: InputV2 },
  { name: 'select', Component: SelectV2 },
  { name: 'textarea', Component: TextareaV2 },
] as const

export function Fixture() {
  return (
    <main>
      {fieldKinds.map(({ name, Component }) => (
        <section key={name}>
          <p id={`${name}-plain-external`}>External plain description</p>
          <Component
            id={`${name}-plain`}
            label={`${name} plain`}
            aria-describedby={`${name}-plain-external`}
            aria-invalid="true"
          >
            {name === 'select' ? <option value="one">One</option> : undefined}
          </Component>

          <p id={`${name}-hint-external`}>External hint description</p>
          <Component
            id={`${name}-hint`}
            label={`${name} hint`}
            hint="Local hint"
            aria-describedby={`${name}-hint-external ${name}-hint-external`}
            aria-invalid="true"
          >
            {name === 'select' ? <option value="one">One</option> : undefined}
          </Component>

          <p id={`${name}-error-external`}>External error description</p>
          <Component
            id={`${name}-error`}
            label={`${name} error`}
            error="Local error"
            aria-describedby={`${name}-error-external`}
            aria-invalid="false"
          >
            {name === 'select' ? <option value="one">One</option> : undefined}
          </Component>
        </section>
      ))}
    </main>
  )
}

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <Fixture />
  </StrictMode>,
)
