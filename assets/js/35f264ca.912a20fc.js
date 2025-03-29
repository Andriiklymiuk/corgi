"use strict";(self.webpackChunkcorgi_docs=self.webpackChunkcorgi_docs||[]).push([[7321],{8860:(e,r,t)=>{t.d(r,{xA:()=>s,yg:()=>d});var n=t(7953);function o(e,r,t){return r in e?Object.defineProperty(e,r,{value:t,enumerable:!0,configurable:!0,writable:!0}):e[r]=t,e}function i(e,r){var t=Object.keys(e);if(Object.getOwnPropertySymbols){var n=Object.getOwnPropertySymbols(e);r&&(n=n.filter((function(r){return Object.getOwnPropertyDescriptor(e,r).enumerable}))),t.push.apply(t,n)}return t}function l(e){for(var r=1;r<arguments.length;r++){var t=null!=arguments[r]?arguments[r]:{};r%2?i(Object(t),!0).forEach((function(r){o(e,r,t[r])})):Object.getOwnPropertyDescriptors?Object.defineProperties(e,Object.getOwnPropertyDescriptors(t)):i(Object(t)).forEach((function(r){Object.defineProperty(e,r,Object.getOwnPropertyDescriptor(t,r))}))}return e}function a(e,r){if(null==e)return{};var t,n,o=function(e,r){if(null==e)return{};var t,n,o={},i=Object.keys(e);for(n=0;n<i.length;n++)t=i[n],r.indexOf(t)>=0||(o[t]=e[t]);return o}(e,r);if(Object.getOwnPropertySymbols){var i=Object.getOwnPropertySymbols(e);for(n=0;n<i.length;n++)t=i[n],r.indexOf(t)>=0||Object.prototype.propertyIsEnumerable.call(e,t)&&(o[t]=e[t])}return o}var c=n.createContext({}),p=function(e){var r=n.useContext(c),t=r;return e&&(t="function"==typeof e?e(r):l(l({},r),e)),t},s=function(e){var r=p(e.components);return n.createElement(c.Provider,{value:r},e.children)},u="mdxType",m={inlineCode:"code",wrapper:function(e){var r=e.children;return n.createElement(n.Fragment,{},r)}},g=n.forwardRef((function(e,r){var t=e.components,o=e.mdxType,i=e.originalType,c=e.parentName,s=a(e,["components","mdxType","originalType","parentName"]),u=p(t),g=o,d=u["".concat(c,".").concat(g)]||u[g]||m[g]||i;return t?n.createElement(d,l(l({ref:r},s),{},{components:t})):n.createElement(d,l({ref:r},s))}));function d(e,r){var t=arguments,o=r&&r.mdxType;if("string"==typeof e||o){var i=t.length,l=new Array(i);l[0]=g;var a={};for(var c in r)hasOwnProperty.call(r,c)&&(a[c]=r[c]);a.originalType=e,a[u]="string"==typeof e?e:o,l[1]=a;for(var p=2;p<i;p++)l[p]=t[p];return n.createElement.apply(null,l)}return n.createElement.apply(null,t)}g.displayName="MDXCreateElement"},9822:(e,r,t)=>{t.r(r),t.d(r,{assets:()=>c,contentTitle:()=>l,default:()=>m,frontMatter:()=>i,metadata:()=>a,toc:()=>p});var n=t(6425),o=(t(7953),t(8860));const i={},l="corgi pull",a={unversionedId:"commands/corgi_pull",id:"commands/corgi_pull",title:"corgi pull",description:"corgi pull",source:"@site/docs/commands/corgi_pull.md",sourceDirName:"commands",slug:"/commands/corgi_pull",permalink:"/corgi/docs/commands/corgi_pull",draft:!1,tags:[],version:"current",frontMatter:{},sidebar:"tutorialSidebar",previous:{title:"corgi list",permalink:"/corgi/docs/commands/corgi_list"},next:{title:"corgi run",permalink:"/corgi/docs/commands/corgi_run"}},c={},p=[{value:"corgi pull",id:"corgi-pull-1",level:2},{value:"Options",id:"options",level:3},{value:"Options inherited from parent commands",id:"options-inherited-from-parent-commands",level:3},{value:"SEE ALSO",id:"see-also",level:3},{value:"Auto generated by spf13/cobra on 2-Sep-2024",id:"auto-generated-by-spf13cobra-on-2-sep-2024",level:6}],s={toc:p},u="wrapper";function m(e){let{components:r,...t}=e;return(0,o.yg)(u,(0,n.A)({},s,t,{components:r,mdxType:"MDXLayout"}),(0,o.yg)("h1",{id:"corgi-pull"},"corgi pull"),(0,o.yg)("h2",{id:"corgi-pull-1"},"corgi pull"),(0,o.yg)("p",null,"Runs git pull for each service folder"),(0,o.yg)("pre",null,(0,o.yg)("code",{parentName:"pre"},"corgi pull [flags]\n")),(0,o.yg)("h3",{id:"options"},"Options"),(0,o.yg)("pre",null,(0,o.yg)("code",{parentName:"pre"},"  -h, --help   help for pull\n")),(0,o.yg)("h3",{id:"options-inherited-from-parent-commands"},"Options inherited from parent commands"),(0,o.yg)("pre",null,(0,o.yg)("code",{parentName:"pre"},'      --describe                  Describe contents of corgi-compose file\n      --dockerContext string      Specify docker context to use, can be default,orbctl,colima (default "default")\n  -l, --exampleList               List examples to choose from. Click on any example to download it\n  -f, --filename string           Custom filepath for for corgi-compose\n      --fromScratch               Clean corgi_services folder before running\n  -t, --fromTemplate string       Create corgi service from template url\n      --fromTemplateName string   Create corgi service from template name and url\n  -g, --global                    Use global path to one of the services\n      --privateToken string       Private token for private repositories to download files\n      --runOnce                   Run corgi once and exit\n      --silent                    Hide all welcome messages\n')),(0,o.yg)("h3",{id:"see-also"},"SEE ALSO"),(0,o.yg)("ul",null,(0,o.yg)("li",{parentName:"ul"},(0,o.yg)("a",{parentName:"li",href:"corgi"},"corgi"),"\t - Corgi cli magic friend")),(0,o.yg)("h6",{id:"auto-generated-by-spf13cobra-on-2-sep-2024"},"Auto generated by spf13/cobra on 2-Sep-2024"))}m.isMDXComponent=!0}}]);