import { getJSON, esc, postLink, tagsHTML } from '/js/common.js';

const titleEl = document.getElementById('series-title');
const contentEl = document.getElementById('series-content');

const slug = decodeURIComponent(location.pathname.replace(/^\/series\//, '').replace(/\.html$/, ''));

try {
  const d = await getJSON('/series/' + encodeURIComponent(slug) + '.json');
  document.title = d.title + ' - zal-blog';
  titleEl.textContent = d.title;

  let head = '';
  if (d.start && d.latest) {
    head += `<span class="dim">${esc(d.start === d.latest ? d.start : d.start + ' ~ ' + d.latest)}</span>`;
  } else if (d.latest) {
    head += `<span class="dim">${esc(d.latest)}</span>`;
  }
  const tags = tagsHTML(d.tags);
  if (tags) head += tags;

  const intro = d.intro ? `<div class="content">${d.intro}</div>` : '';
  const chapters = (d.posts && d.posts.length)
    ? d.posts.map(p => {
        const num = p.order ? `${String(p.order).padStart(2, '0')} ` : '';
        return `<a href="${postLink(p.path)}">${num}${esc(p.title)}</a>` +
               `<span class="dim">${esc(p.date)}</span>`;
      }).join('\n')
    : 'no chapters yet';

  contentEl.innerHTML =
    (head ? head + '\n' : '') +
    intro +
    `<div class="content">${d.intro ? '<hr>' : ''}${chapters}</div>`;
} catch (e) {
  contentEl.textContent = '[Error] ' + e.message;
}