"use strict";(self.webpackChunkcorgi_docs=self.webpackChunkcorgi_docs||[]).push([[7299],{939:(e,n,o)=>{o.r(n),o.d(n,{assets:()=>s,contentTitle:()=>i,default:()=>p,frontMatter:()=>t,metadata:()=>l,toc:()=>A});var r=o(6425),a=(o(7953),o(8860));const t={sidebar_position:5},i="How to run db helpers",l={unversionedId:"db_helpers",id:"db_helpers",title:"How to run db helpers",description:"Db everywhere",source:"@site/docs/db_helpers.md",sourceDirName:".",slug:"/db_helpers",permalink:"/corgi/docs/db_helpers",draft:!1,tags:[],version:"current",sidebarPosition:5,frontMatter:{sidebar_position:5},sidebar:"tutorialSidebar",previous:{title:"corgi upgrade",permalink:"/corgi/docs/commands/corgi_upgrade"},next:{title:"Create database dump",permalink:"/corgi/docs/database_dump"}},s={},A=[{value:"Database seeding",id:"database-seeding",level:2}],c={toc:A},d="wrapper";function p(e){let{components:n,...t}=e;return(0,a.yg)(d,(0,r.A)({},c,t,{components:n,mdxType:"MDXLayout"}),(0,a.yg)("h1",{id:"how-to-run-db-helpers"},"How to run db helpers"),(0,a.yg)("p",null,(0,a.yg)("img",{alt:"Db everywhere",src:o(3638).A,width:"300",height:"225"})),(0,a.yg)("p",null,"These are helpers command for your db_services."),(0,a.yg)("pre",null,(0,a.yg)("code",{parentName:"pre",className:"language-bash"},"  # this will show you help message\n  corgi\n\n  # before running db commands you need to create corgi-compose.yml file, add services config there and run corgi init, so that there is db_services folder, that is created\n\n  # example to run db service\n  corgi db\n\n  # example to show help commands\n  corgi db -h\n  corgi -h\n")),(0,a.yg)("p",null,"You can run cli with flags, without specifying service, to do smth with all\ndatabases, for example:"),(0,a.yg)("pre",null,(0,a.yg)("code",{parentName:"pre",className:"language-bash"},"  # run db service and start all databases\n  corgi db -u\n  # similar to\n  corgi db -upAll\n\n  # stop, remove and start all databases \n  corgi db  -r -s -u\n  # similar to\n  corgi db  -rsu\n  # similar to\n  corgi db  -removeAll -stopAll -upAll\n")),(0,a.yg)("p",null,"You can run each service individually, e.g. ",(0,a.yg)("inlineCode",{parentName:"p"},"corgi db"),". It will show you\ninteractive menu to choose one of the service databases, that are located in\n",(0,a.yg)("inlineCode",{parentName:"p"},"corgi_services/db_services")," folder."),(0,a.yg)("pre",null,(0,a.yg)("code",{parentName:"pre",className:"language-bash"},"Use the arrow keys to navigate: \u2193 \u2191 \u2192 \u2190 \n? Select service: \n  \u25b8 \ud83d\uded1  close program\n    analytics\n    backend\n    backoffice\n")),(0,a.yg)("p",null,"This menu helps to choose target service and its commands, that are located in\nMakefile of targeted service (we choose backoffice service for example)"),(0,a.yg)("pre",null,(0,a.yg)("code",{parentName:"pre",className:"language-bash"},"Connection info to backoffice:\n\nPORT 5432\nUSER corgi\nPASSWORD corgiPassword\nDB corgi-adm\n\nbackoffice ist running \ud83d\udd34\nUse the arrow keys to navigate: \u2193 \u2191 \u2192 \u2190 \n? Select command: \n  \u25b8 \u2b05\ufe0f  go back\n    down\n    help\n    id\n    listDocker\n    seed\n\u2193   up\n")),(0,a.yg)("h2",{id:"database-seeding"},"Database seeding"),(0,a.yg)("p",null,"If you want to do seeding manually to do database seeding (population with\ndata), you can do it:"),(0,a.yg)("ul",null,(0,a.yg)("li",{parentName:"ul"},(0,a.yg)("h3",{parentName:"li",id:"automatically-recommended"},"Automatically (",(0,a.yg)("strong",{parentName:"h3"},"recommended"),")"))),(0,a.yg)("p",null,"Add seedSource in ",(0,a.yg)("inlineCode",{parentName:"p"},"corgi-compose.yml")," and then run"),(0,a.yg)("pre",null,(0,a.yg)("code",{parentName:"pre",className:"language-bash"},"corgi run --seed\n")),(0,a.yg)("p",null,"It will create dump of data and then seed it to your database."),(0,a.yg)("p",null,(0,a.yg)("strong",{parentName:"p"},"Tip")," You can add in ",(0,a.yg)("inlineCode",{parentName:"p"},"seedFromDbEnvPath")," the path to env config with db in the\nform of:"),(0,a.yg)("pre",null,(0,a.yg)("code",{parentName:"pre"},"DB_HOST=host_of_db_from_which_to_get_dump\nDB_NAME=name_of_db_from_which_to_get_dump\nDB_USER=user_of_db_from_which_to_get_dump\nDB_PASSWORD=password_of_db_from_which_to_get_dump\nDB_PORT=port_of_db_from_which_to_get_dump\n")),(0,a.yg)("ul",null,(0,a.yg)("li",{parentName:"ul"},(0,a.yg)("h3",{parentName:"li",id:"manually"},"Manually"))),(0,a.yg)("p",null,"If you provided seedSource in ",(0,a.yg)("inlineCode",{parentName:"p"},"corgi-compose.yml"),", than you can do:"),(0,a.yg)("ol",null,(0,a.yg)("li",{parentName:"ol"},(0,a.yg)("inlineCode",{parentName:"li"},"corgi db"),"-> choose service -> Choose ",(0,a.yg)("strong",{parentName:"li"},"dump")),(0,a.yg)("li",{parentName:"ol"},(0,a.yg)("inlineCode",{parentName:"li"},"corgi db"),"-> choose service -> Choose ",(0,a.yg)("strong",{parentName:"li"},"seed"))),(0,a.yg)("p",null,"If no seedSource provided:"),(0,a.yg)("ol",{start:0},(0,a.yg)("li",{parentName:"ol"},(0,a.yg)("a",{parentName:"li",href:"/corgi/docs/database_dump"},"Create database dump"),", name it ",(0,a.yg)("inlineCode",{parentName:"li"},"dump.sql")," and place it\nin targeted service, e.g. place it in ",(0,a.yg)("inlineCode",{parentName:"li"},"corgi_services/db_services/backoffice"),"\nfolder"),(0,a.yg)("li",{parentName:"ol"},"Run ",(0,a.yg)("inlineCode",{parentName:"li"},"corgi db")," from root folder"),(0,a.yg)("li",{parentName:"ol"},"Choose service"),(0,a.yg)("li",{parentName:"ol"},"Choose seed. It will populate db.")),(0,a.yg)("p",null,(0,a.yg)("strong",{parentName:"p"},"Important"),": seeding is best to do on newly created db."),(0,a.yg)("p",null,(0,a.yg)("a",{parentName:"p",href:"/docs/intro"},"Main docs")))}p.isMDXComponent=!0},3638:(e,n,o)=>{o.d(n,{A:()=>r});const r="data:image/jpeg;base64,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"},8860:(e,n,o)=>{o.d(n,{xA:()=>c,yg:()=>g});var r=o(7953);function a(e,n,o){return n in e?Object.defineProperty(e,n,{value:o,enumerable:!0,configurable:!0,writable:!0}):e[n]=o,e}function t(e,n){var o=Object.keys(e);if(Object.getOwnPropertySymbols){var r=Object.getOwnPropertySymbols(e);n&&(r=r.filter((function(n){return Object.getOwnPropertyDescriptor(e,n).enumerable}))),o.push.apply(o,r)}return o}function i(e){for(var n=1;n<arguments.length;n++){var o=null!=arguments[n]?arguments[n]:{};n%2?t(Object(o),!0).forEach((function(n){a(e,n,o[n])})):Object.getOwnPropertyDescriptors?Object.defineProperties(e,Object.getOwnPropertyDescriptors(o)):t(Object(o)).forEach((function(n){Object.defineProperty(e,n,Object.getOwnPropertyDescriptor(o,n))}))}return e}function l(e,n){if(null==e)return{};var o,r,a=function(e,n){if(null==e)return{};var o,r,a={},t=Object.keys(e);for(r=0;r<t.length;r++)o=t[r],n.indexOf(o)>=0||(a[o]=e[o]);return a}(e,n);if(Object.getOwnPropertySymbols){var t=Object.getOwnPropertySymbols(e);for(r=0;r<t.length;r++)o=t[r],n.indexOf(o)>=0||Object.prototype.propertyIsEnumerable.call(e,o)&&(a[o]=e[o])}return a}var s=r.createContext({}),A=function(e){var n=r.useContext(s),o=n;return e&&(o="function"==typeof e?e(n):i(i({},n),e)),o},c=function(e){var n=A(e.components);return r.createElement(s.Provider,{value:n},e.children)},d="mdxType",p={inlineCode:"code",wrapper:function(e){var n=e.children;return r.createElement(r.Fragment,{},n)}},m=r.forwardRef((function(e,n){var o=e.components,a=e.mdxType,t=e.originalType,s=e.parentName,c=l(e,["components","mdxType","originalType","parentName"]),d=A(o),m=a,g=d["".concat(s,".").concat(m)]||d[m]||p[m]||t;return o?r.createElement(g,i(i({ref:n},c),{},{components:o})):r.createElement(g,i({ref:n},c))}));function g(e,n){var o=arguments,a=n&&n.mdxType;if("string"==typeof e||a){var t=o.length,i=new Array(t);i[0]=m;var l={};for(var s in n)hasOwnProperty.call(n,s)&&(l[s]=n[s]);l.originalType=e,l[d]="string"==typeof e?e:a,i[1]=l;for(var A=2;A<t;A++)i[A]=o[A];return r.createElement.apply(null,i)}return r.createElement.apply(null,o)}m.displayName="MDXCreateElement"}}]);